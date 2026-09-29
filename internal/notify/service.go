package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	ActionChannelCreated = "NOTIFICATION_CHANNEL_CREATED"
	ActionChannelUpdated = "NOTIFICATION_CHANNEL_UPDATED"
	ActionChannelDeleted = "NOTIFICATION_CHANNEL_DELETED"
	ActionChannelTested  = "NOTIFICATION_CHANNEL_TESTED"
)

// Service manages notification channels for the API. Deliveries are sent by Worker.
type Service struct {
	Store  *store.Store
	Sealer *secrets.Sealer
	Log    *slog.Logger
	Deps   Deps
}

func NewService(st *store.Store, sealer *secrets.Sealer, deps Deps, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	if deps.HTTP == nil {
		deps.HTTP = NewHTTPClient(sendTimeout)
	}
	return &Service{Store: st, Sealer: sealer, Log: log, Deps: deps}
}

// Channel is the API view of a channel: secrets are never included, only which ones are set.
type Channel struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Name       string
	Type       Type
	Enabled    bool
	Config     Config
	SecretsSet []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type CreateRequest struct {
	Name    string
	Type    Type
	Enabled bool
	Config  Config
	Secrets Secrets
}

type UpdateRequest struct {
	Name         *string
	Enabled      *bool
	Config       *Config
	Secrets      *Secrets
	ClearSecrets []string
}

// Delivery is one outbox row as shown to administrators.
type Delivery struct {
	ID             uuid.UUID
	ChannelID      *uuid.UUID
	ChannelName    string
	ChannelType    string
	RuleID         *uuid.UUID
	NotificationID *uuid.UUID
	Destination    string
	Status         string
	Attempts       int
	LastError      *string
	NextAttemptAt  time.Time
	CreatedAt      time.Time
	SentAt         *time.Time
}

func (s *Service) require(ctx context.Context, q *db.Queries, actor authz.Actor) error {
	chk, err := access.Load(ctx, q, actor)
	if err != nil {
		return err
	}
	return chk.Require(authz.NotificationsManage, access.Tenant(*actor.TenantID))
}

func (s *Service) tx(ctx context.Context, actor authz.Actor, fn func(q *db.Queries) error) error {
	if actor.TenantID == nil {
		return &access.ForbiddenError{Permission: authz.NotificationsManage}
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		if err := s.require(ctx, q, actor); err != nil {
			return err
		}
		return fn(q)
	})
}

func (s *Service) openSecrets(row db.NotificationChannel) (Secrets, error) {
	return OpenSecrets(s.Sealer, row)
}

// OpenSecrets decrypts a channel's sealed secrets; a channel without any yields the zero value.
func OpenSecrets(sealer *secrets.Sealer, row db.NotificationChannel) (Secrets, error) {
	var out Secrets
	if len(row.SecretsSealed) == 0 {
		return out, nil
	}
	plain, err := sealer.Open(row.SecretsSealed, row.ID[:])
	if err != nil {
		return out, fmt.Errorf("open channel secrets: %w", err)
	}
	if err := json.Unmarshal(plain, &out); err != nil {
		return out, fmt.Errorf("decode channel secrets: %w", err)
	}
	return out, nil
}

func (s *Service) sealSecrets(id uuid.UUID, sec Secrets) ([]byte, error) {
	plain, err := json.Marshal(sec)
	if err != nil {
		return nil, err
	}
	return s.Sealer.Seal(plain, id[:])
}

func decodeConfig(row db.NotificationChannel) Config {
	var c Config
	_ = json.Unmarshal(row.Config, &c)
	return c
}

func (s *Service) view(row db.NotificationChannel) (Channel, error) {
	sec, err := s.openSecrets(row)
	if err != nil {
		return Channel{}, err
	}
	return Channel{
		ID: row.ID, TenantID: row.TenantID, Name: row.Name, Type: Type(row.Type), Enabled: row.Enabled,
		Config: decodeConfig(row), SecretsSet: sec.Set(), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func (s *Service) List(ctx context.Context, actor authz.Actor) ([]Channel, error) {
	var out []Channel
	err := s.tx(ctx, actor, func(q *db.Queries) error {
		rows, err := q.ListNotificationChannels(ctx, *actor.TenantID)
		if err != nil {
			return store.Classify(err)
		}
		out = make([]Channel, 0, len(rows))
		for _, r := range rows {
			v, err := s.view(r)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

func (s *Service) Get(ctx context.Context, actor authz.Actor, id uuid.UUID) (Channel, error) {
	var out Channel
	err := s.tx(ctx, actor, func(q *db.Queries) error {
		row, err := q.GetNotificationChannel(ctx, db.GetNotificationChannelParams{ID: id, TenantID: *actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		out, err = s.view(row)
		return err
	})
	return out, err
}

func (s *Service) Create(ctx context.Context, actor authz.Actor, req CreateRequest) (Channel, error) {
	if actor.TenantID == nil {
		return Channel{}, &access.ForbiddenError{Permission: authz.NotificationsManage}
	}
	name := strings.TrimSpace(req.Name)
	id := uuid.New()
	var out Channel
	// Validation runs inside the transaction, after the permission check, so an unauthorized
	// caller learns nothing about validity.
	err := s.tx(ctx, actor, func(q *db.Queries) error {
		if name == "" || len(name) > 100 {
			return &ValidationError{Msg: "name is required (at most 100 characters)"}
		}
		cfg, err := ValidateConfig(req.Type, req.Config, req.Secrets)
		if err != nil {
			return err
		}
		cfgBytes, _ := json.Marshal(cfg)
		var sealed []byte
		if len(req.Secrets.Set()) > 0 {
			if sealed, err = s.sealSecrets(id, req.Secrets); err != nil {
				return err
			}
		}
		row, err := q.CreateNotificationChannel(ctx, db.CreateNotificationChannelParams{
			ID: id, TenantID: *actor.TenantID, Name: name, Type: string(req.Type),
			Config: cfgBytes, SecretsSealed: sealed, Enabled: req.Enabled,
		})
		if err != nil {
			return store.Classify(err)
		}
		if out, err = s.view(row); err != nil {
			return err
		}
		return audit(ctx, q, actor, ActionChannelCreated, id, map[string]any{
			"name": name, "type": req.Type, "secrets_set": out.SecretsSet,
		})
	})
	return out, err
}

func (s *Service) Update(ctx context.Context, actor authz.Actor, id uuid.UUID, req UpdateRequest) (Channel, error) {
	if actor.TenantID == nil {
		return Channel{}, &access.ForbiddenError{Permission: authz.NotificationsManage}
	}
	var name *string
	var out Channel
	err := s.tx(ctx, actor, func(q *db.Queries) error {
		if req.Name != nil {
			n := strings.TrimSpace(*req.Name)
			if n == "" || len(n) > 100 {
				return &ValidationError{Msg: "name is required (at most 100 characters)"}
			}
			name = &n
		}
		row, err := q.GetNotificationChannel(ctx, db.GetNotificationChannelParams{ID: id, TenantID: *actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		cur, err := s.openSecrets(row)
		if err != nil {
			return err
		}
		sec := cur
		secretsChanged := len(req.ClearSecrets) > 0 || req.Secrets != nil
		if secretsChanged {
			var in Secrets
			if req.Secrets != nil {
				in = *req.Secrets
			}
			sec = cur.Merge(in, req.ClearSecrets)
		}
		cfg := decodeConfig(row)
		if req.Config != nil {
			cfg = *req.Config
		}
		if req.Config != nil || secretsChanged {
			if cfg, err = ValidateConfig(Type(row.Type), cfg, sec); err != nil {
				return err
			}
		}
		p := db.UpdateNotificationChannelParams{ID: id, TenantID: *actor.TenantID, Name: name, Enabled: req.Enabled}
		if req.Config != nil {
			p.Config, _ = json.Marshal(cfg)
		}
		if secretsChanged {
			if p.SecretsSealed, err = s.sealSecrets(id, sec); err != nil {
				return err
			}
		}
		updated, err := q.UpdateNotificationChannel(ctx, p)
		if err != nil {
			return store.Classify(err)
		}
		if out, err = s.view(updated); err != nil {
			return err
		}
		return audit(ctx, q, actor, ActionChannelUpdated, id, map[string]any{
			"name": out.Name, "type": out.Type, "enabled": out.Enabled, "secrets_set": out.SecretsSet,
		})
	})
	return out, err
}

// Delete removes the channel, strips it from every rule that used it and keeps its delivery history.
func (s *Service) Delete(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries) error {
		row, err := q.GetNotificationChannel(ctx, db.GetNotificationChannelParams{ID: id, TenantID: *actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		if err := q.StripChannelFromRules(ctx, db.StripChannelFromRulesParams{ChannelID: id.String(), TenantID: *actor.TenantID}); err != nil {
			return store.Classify(err)
		}
		if err := q.DeleteNotificationChannel(ctx, db.DeleteNotificationChannelParams{ID: id, TenantID: *actor.TenantID}); err != nil {
			return store.Classify(err)
		}
		return audit(ctx, q, actor, ActionChannelDeleted, id, map[string]any{"name": row.Name, "type": row.Type})
	})
}

func audit(ctx context.Context, q *db.Queries, actor authz.Actor, action string, targetID uuid.UUID, details map[string]any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var aid *uuid.UUID
	if actor.UserID != uuid.Nil {
		aid = &actor.UserID
	}
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: actor.TenantID, ActorID: aid, Action: action,
		TargetType: "notification_channel", TargetID: &targetID, Details: b,
	})
}
