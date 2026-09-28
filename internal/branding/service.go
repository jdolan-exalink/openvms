// Package branding manages per-tenant owner branding (display name and logo), configured
// from the Configuración area and burned into every plate detail photo/clip download as a
// watermark (date/time + owner name/logo), and rendered as an on-screen CSS overlay while
// viewing. Reading branding needs no special permission beyond belonging to the tenant (or
// being a platform user): every viewer of a plate detail needs it to render the overlay.
// Changing it needs tenant.manage, the closest existing tenant-administration permission
// (internal/authz/catalog.go has no dedicated "branding" permission).
package branding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // format sniffing only
	_ "image/png"  // format sniffing only
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Audit actions (PRD §66 naming convention).
const (
	ActionBrandingUpdated = "BRANDING_UPDATED"
	ActionBrandingRemoved = "BRANDING_REMOVED"
)

// MaxLogoBytes limits the uploaded logo image, validated server-side.
const MaxLogoBytes = 512 << 10 // 512 KB

// MaxLogoPixels bounds the logo's *declared* pixel count (width * height), independent of its
// encoded byte size. PDW-6 finding: image.DecodeConfig only reads the header, so a highly
// compressible solid-color PNG can declare an enormous width/height while staying well under
// MaxLogoBytes on disk — a classic decompression bomb. Without this bound, that file would
// pass validation and later be fully decoded (BurnPhoto for photo downloads, and again when
// staged for ffmpeg's overlay filter for clip jobs), allocating gigabytes of pixel data for a
// tiny upload. 1024x1024 is already generous for a logo meant to sit in the corner of a
// watermark bar a few dozen pixels tall.
const MaxLogoPixels = 1024 * 1024

// Blobs stores the logo bytes; *objectstore.Store implements it.
type Blobs interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, string, error)
}

type Service struct {
	Store *store.Store
	Blobs Blobs
	Log   *slog.Logger
}

// ValidationError is returned for a well-formed but unacceptable request.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// Branding is one tenant's configured owner branding.
type Branding struct {
	TenantID        uuid.UUID
	OwnerName       string
	HasLogo         bool
	LogoContentType string
	UpdatedAt       time.Time
}

func logoKey(tenantID uuid.UUID) string {
	return "tenant/" + tenantID.String() + "/branding/logo"
}

func toBranding(row db.TenantBranding) Branding {
	return Branding{
		TenantID: row.TenantID, OwnerName: row.OwnerName,
		HasLogo: row.LogoKey != "", LogoContentType: row.LogoContentType, UpdatedAt: row.UpdatedAt,
	}
}

// authorizeTenant checks the actor may reach tenantID at all: their own tenant, or any
// tenant for a platform user. Another tenant answers store.ErrNotFound (never leak that it
// exists), matching the rest of the codebase's cross-tenant convention.
func authorizeTenant(actor authz.Actor, tenantID uuid.UUID) error {
	if !actor.IsPlatform() && *actor.TenantID != tenantID {
		return store.ErrNotFound
	}
	return nil
}

func (s *Service) tx(ctx context.Context, actor authz.Actor, fn func(q *db.Queries, c *access.Checker) error) error {
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		c, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		return fn(q, c)
	})
}

// getRow reads the raw row, translating "no branding configured yet" (pgx.ErrNoRows) into
// ok=false rather than an error: branding is optional, so a missing row is not a fault.
func getRow(ctx context.Context, q *db.Queries, tenantID uuid.UUID) (db.TenantBranding, bool, error) {
	row, err := q.GetTenantBranding(ctx, tenantID)
	if err != nil {
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			return db.TenantBranding{}, false, nil
		}
		return db.TenantBranding{}, false, err
	}
	return row, true, nil
}

// Get returns tenantID's branding. A tenant with no configured branding returns a zero
// Branding (empty owner name, no logo), not an error: branding is optional.
func (s *Service) Get(ctx context.Context, actor authz.Actor, tenantID uuid.UUID) (Branding, error) {
	if err := authorizeTenant(actor, tenantID); err != nil {
		return Branding{}, err
	}
	out := Branding{TenantID: tenantID}
	err := s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		row, ok, err := getRow(ctx, q, tenantID)
		if err != nil {
			return err
		}
		if ok {
			out = toBranding(row)
		}
		return nil
	})
	return out, err
}

// Logo returns the raw logo bytes and content type, or store.ErrNotFound if none is set.
func (s *Service) Logo(ctx context.Context, actor authz.Actor, tenantID uuid.UUID) ([]byte, string, error) {
	if err := authorizeTenant(actor, tenantID); err != nil {
		return nil, "", err
	}
	var key string
	err := s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		row, ok, err := getRow(ctx, q, tenantID)
		if err != nil {
			return err
		}
		if ok {
			key = row.LogoKey
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if key == "" {
		return nil, "", store.ErrNotFound
	}
	return s.Blobs.Get(ctx, key)
}

// Input is a partial update: nil/zero fields keep the current value. RemoveLogo clears the
// logo regardless of Logo/LogoContentType.
type Input struct {
	OwnerName       *string
	Logo            []byte
	LogoContentType string
	RemoveLogo      bool
}

func decodeAndValidateLogo(data []byte, contentType string) error {
	if len(data) == 0 {
		return invalid("logo image is empty")
	}
	if len(data) > MaxLogoBytes {
		return invalid("logo image exceeds %d KB", MaxLogoBytes/1024)
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct != "image/png" && ct != "image/jpeg" {
		return invalid("logo must be PNG or JPEG")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return invalid("logo is not a valid image: %s", err)
	}
	if (format != "png" && format != "jpeg") || cfg.Width <= 0 || cfg.Height <= 0 {
		return invalid("logo must be a valid PNG or JPEG image")
	}
	// PDW-6: bound declared pixel count, not just encoded byte size — see MaxLogoPixels.
	if int64(cfg.Width)*int64(cfg.Height) > MaxLogoPixels {
		return invalid("logo dimensions (%dx%d) are too large (max %d pixels)", cfg.Width, cfg.Height, MaxLogoPixels)
	}
	// The declared Content-Type must match the sniffed bytes: a caller cannot smuggle a
	// JPEG in under an image/png declaration (or vice versa) past this check.
	if (format == "png" && ct != "image/png") || (format == "jpeg" && ct != "image/jpeg") {
		return invalid("logo is not a valid image: declared content type %s does not match the file", ct)
	}
	return nil
}

// Update applies a partial change to tenantID's branding (tenant.manage).
func (s *Service) Update(ctx context.Context, actor authz.Actor, tenantID uuid.UUID, in Input) (Branding, error) {
	if err := authorizeTenant(actor, tenantID); err != nil {
		return Branding{}, err
	}
	if in.OwnerName != nil {
		trimmed := strings.TrimSpace(*in.OwnerName)
		if len(trimmed) > 200 {
			return Branding{}, invalid("owner name is too long (max 200 characters)")
		}
		in.OwnerName = &trimmed
	}
	if in.RemoveLogo && len(in.Logo) > 0 {
		return Branding{}, invalid("cannot set and remove the logo in the same request")
	}
	if len(in.Logo) > 0 {
		if err := decodeAndValidateLogo(in.Logo, in.LogoContentType); err != nil {
			return Branding{}, err
		}
	}

	var out Branding
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.TenantManage, access.Tenant(tenantID)); err != nil {
			return err
		}
		current, _, err := getRow(ctx, q, tenantID)
		if err != nil {
			return err
		}
		ownerName := current.OwnerName
		if in.OwnerName != nil {
			ownerName = *in.OwnerName
		}
		logoKeyVal, logoContentType := current.LogoKey, current.LogoContentType
		switch {
		case in.RemoveLogo:
			logoKeyVal, logoContentType = "", ""
		case len(in.Logo) > 0:
			logoKeyVal = logoKey(tenantID)
			logoContentType = strings.ToLower(strings.TrimSpace(in.LogoContentType))
		}
		row, err := q.UpsertTenantBranding(ctx, db.UpsertTenantBrandingParams{
			TenantID: tenantID, OwnerName: ownerName, LogoKey: logoKeyVal, LogoContentType: logoContentType,
			UpdatedBy: &actor.UserID,
		})
		if err != nil {
			return store.Classify(err)
		}
		details := map[string]any{"owner_name": ownerName, "has_logo": logoKeyVal != ""}
		b, _ := json.Marshal(details)
		if err := q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: &tenantID, ActorID: &actor.UserID, ActorName: actor.Username, Action: ActionBrandingUpdated,
			TargetType: "tenant", TargetID: &tenantID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: b,
		}); err != nil {
			return err
		}
		out = toBranding(row)
		return nil
	})
	if err != nil {
		return Branding{}, err
	}
	// The logo bytes are written to the object store only after the transaction commits the
	// metadata row, so a failed upload never leaves a dangling logo_key pointing at nothing;
	// a failed upload after a successful commit is reported to the caller, who can retry.
	if len(in.Logo) > 0 {
		if err := s.Blobs.Put(ctx, logoKey(tenantID), in.Logo, strings.ToLower(strings.TrimSpace(in.LogoContentType))); err != nil {
			return Branding{}, fmt.Errorf("store logo: %w", err)
		}
	}
	return out, nil
}

// Delete clears tenantID's branding entirely (owner name and logo), leaving the tenant with
// no watermark beyond the timestamp (tenant.manage).
func (s *Service) Delete(ctx context.Context, actor authz.Actor, tenantID uuid.UUID) error {
	if err := authorizeTenant(actor, tenantID); err != nil {
		return err
	}
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.TenantManage, access.Tenant(tenantID)); err != nil {
			return err
		}
		if _, err := q.UpsertTenantBranding(ctx, db.UpsertTenantBrandingParams{
			TenantID: tenantID, OwnerName: "", LogoKey: "", LogoContentType: "", UpdatedBy: &actor.UserID,
		}); err != nil {
			return store.Classify(err)
		}
		return q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: &tenantID, ActorID: &actor.UserID, ActorName: actor.Username, Action: ActionBrandingRemoved,
			TargetType: "tenant", TargetID: &tenantID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: []byte("{}"),
		})
	})
}
