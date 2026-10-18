package faces

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Watchlist audit actions. Details hold ids and counts only, never names or descriptions.
const (
	ActionWatchlistCreated = "FACE_WATCHLIST_CREATED"
	ActionWatchlistUpdated = "FACE_WATCHLIST_UPDATED"
	ActionWatchlistDeleted = "FACE_WATCHLIST_DELETED"
	ActionWatchlistRead    = "FACE_WATCHLIST_READ"
)

const (
	// MaxWatchlistNameLen and MaxWatchlistDescriptionLen mirror the table CHECK constraints.
	MaxWatchlistNameLen        = 120
	MaxWatchlistDescriptionLen = 500
	// MaxWatchlistMembers bounds one list (the OpenAPI contract states the same limit).
	MaxWatchlistMembers = 500
)

// Watchlist is a named group of registered persons used by alarm rules.
type Watchlist struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Name        string
	Description string
	PersonIDs   []uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type WatchlistInput struct {
	Name        string
	Description string
	PersonIDs   []uuid.UUID
}

// WatchlistUpdate is a partial update: nil fields keep their value; PersonIDs replaces the members.
type WatchlistUpdate struct {
	Name        *string
	Description *string
	PersonIDs   *[]uuid.UUID
}

func toWatchlist(id, tenantID uuid.UUID, name, description string, personIDs []uuid.UUID, created, updated time.Time) Watchlist {
	if personIDs == nil {
		personIDs = []uuid.UUID{}
	}
	return Watchlist{ID: id, TenantID: tenantID, Name: name, Description: description, PersonIDs: personIDs, CreatedAt: created, UpdatedAt: updated}
}

func cleanWatchlistName(name string) (string, error) {
	n := strings.TrimSpace(name)
	switch {
	case n == "":
		return "", invalid("watchlist name is required")
	case utf8.RuneCountInString(n) > MaxWatchlistNameLen:
		return "", invalid("watchlist name is too long (max %d characters)", MaxWatchlistNameLen)
	}
	return n, nil
}

func cleanWatchlistDescription(d string) (string, error) {
	n := strings.TrimSpace(d)
	if utf8.RuneCountInString(n) > MaxWatchlistDescriptionLen {
		return "", invalid("watchlist description is too long (max %d characters)", MaxWatchlistDescriptionLen)
	}
	return n, nil
}

func uniquePersonIDs(ids []uuid.UUID) ([]uuid.UUID, error) {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) > MaxWatchlistMembers {
		return nil, invalid("a watchlist holds at most %d persons", MaxWatchlistMembers)
	}
	return out, nil
}

// setMembers replaces the members after checking every id is a live person of the tenant.
func setMembers(ctx context.Context, q *db.Queries, tenantID, listID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) > 0 {
		n, err := q.CountFacePersonsByIDs(ctx, db.CountFacePersonsByIDsParams{TenantID: tenantID, Ids: ids})
		if err != nil {
			return store.Classify(err)
		}
		if int(n) != len(ids) {
			return invalid("unknown person in person_ids")
		}
	}
	if err := q.ClearFaceWatchlistMembers(ctx, db.ClearFaceWatchlistMembersParams{TenantID: tenantID, WatchlistID: listID}); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return q.AddFaceWatchlistMembers(ctx, db.AddFaceWatchlistMembersParams{TenantID: tenantID, WatchlistID: listID, PersonIds: ids})
}

func (s *Service) readWatchlist(ctx context.Context, q *db.Queries, tenantID, id uuid.UUID) (Watchlist, error) {
	row, err := q.GetFaceWatchlist(ctx, db.GetFaceWatchlistParams{TenantID: tenantID, ID: id})
	if err != nil {
		return Watchlist{}, store.Classify(err)
	}
	return toWatchlist(row.ID, row.TenantID, row.Name, row.Description, row.PersonIds, row.CreatedAt, row.UpdatedAt), nil
}

// ListWatchlists lists the tenant's watchlists (faces.view).
func (s *Service) ListWatchlists(ctx context.Context, actor authz.Actor, tenantID uuid.UUID) ([]Watchlist, error) {
	out := []Watchlist{}
	err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		rows, err := q.ListFaceWatchlists(ctx, tenantID)
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, toWatchlist(r.ID, r.TenantID, r.Name, r.Description, r.PersonIds, r.CreatedAt, r.UpdatedAt))
		}
		return s.audit(ctx, q, actor, tenantID, ActionWatchlistRead, "face_watchlist", uuid.Nil, map[string]any{"scope": "list", "count": len(out)})
	})
	return out, err
}

// GetWatchlist reads one watchlist (faces.view).
func (s *Service) GetWatchlist(ctx context.Context, actor authz.Actor, tenantID, id uuid.UUID) (Watchlist, error) {
	var out Watchlist
	err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		var err error
		if out, err = s.readWatchlist(ctx, q, tenantID, id); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionWatchlistRead, "face_watchlist", id, map[string]any{"scope": "detail", "watchlist_id": id})
	})
	return out, err
}

// CreateWatchlist creates a watchlist (faces.manage).
func (s *Service) CreateWatchlist(ctx context.Context, actor authz.Actor, tenantID uuid.UUID, in WatchlistInput) (Watchlist, error) {
	name, err := cleanWatchlistName(in.Name)
	if err != nil {
		return Watchlist{}, err
	}
	desc, err := cleanWatchlistDescription(in.Description)
	if err != nil {
		return Watchlist{}, err
	}
	ids, err := uniquePersonIDs(in.PersonIDs)
	if err != nil {
		return Watchlist{}, err
	}
	var out Watchlist
	err = s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		row, err := q.CreateFaceWatchlist(ctx, db.CreateFaceWatchlistParams{TenantID: tenantID, Name: name, Description: desc, CreatedBy: &actor.UserID})
		if err != nil {
			return store.Classify(err)
		}
		if err := setMembers(ctx, q, tenantID, row.ID, ids); err != nil {
			return err
		}
		if out, err = s.readWatchlist(ctx, q, tenantID, row.ID); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionWatchlistCreated, "face_watchlist", row.ID, map[string]any{"watchlist_id": row.ID, "members": len(ids)})
	})
	return out, err
}

// UpdateWatchlist renames a watchlist, edits its description or replaces its members (faces.manage).
func (s *Service) UpdateWatchlist(ctx context.Context, actor authz.Actor, tenantID, id uuid.UUID, in WatchlistUpdate) (Watchlist, error) {
	var name, desc *string
	if in.Name != nil {
		n, err := cleanWatchlistName(*in.Name)
		if err != nil {
			return Watchlist{}, err
		}
		name = &n
	}
	if in.Description != nil {
		d, err := cleanWatchlistDescription(*in.Description)
		if err != nil {
			return Watchlist{}, err
		}
		desc = &d
	}
	var ids []uuid.UUID
	if in.PersonIDs != nil {
		var err error
		if ids, err = uniquePersonIDs(*in.PersonIDs); err != nil {
			return Watchlist{}, err
		}
	}
	var out Watchlist
	err := s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		cur, err := q.GetFaceWatchlist(ctx, db.GetFaceWatchlistParams{TenantID: tenantID, ID: id})
		if err != nil {
			return store.Classify(err)
		}
		next := db.UpdateFaceWatchlistParams{TenantID: tenantID, ID: id, Name: cur.Name, Description: cur.Description, UpdatedBy: &actor.UserID}
		changed := []string{}
		if name != nil && *name != cur.Name {
			next.Name = *name
			changed = append(changed, "name")
		}
		if desc != nil && *desc != cur.Description {
			next.Description = *desc
			changed = append(changed, "description")
		}
		if _, err := q.UpdateFaceWatchlist(ctx, next); err != nil {
			return store.Classify(err)
		}
		if in.PersonIDs != nil {
			if err := setMembers(ctx, q, tenantID, id, ids); err != nil {
				return err
			}
			changed = append(changed, "members")
		}
		if out, err = s.readWatchlist(ctx, q, tenantID, id); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionWatchlistUpdated, "face_watchlist", id, map[string]any{"watchlist_id": id, "changed": changed, "members": len(out.PersonIDs)})
	})
	return out, err
}

// DeleteWatchlist removes a watchlist (faces.manage). A list an alarm rule still uses is a
// conflict: deleting it would leave the rule matching nobody.
func (s *Service) DeleteWatchlist(ctx context.Context, actor authz.Actor, tenantID, id uuid.UUID) error {
	return s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		if _, err := q.GetFaceWatchlist(ctx, db.GetFaceWatchlistParams{TenantID: tenantID, ID: id}); err != nil {
			return store.Classify(err)
		}
		// Wait for any rule create/update that holds the list, so the count below includes it.
		if _, err := q.LockFaceWatchlist(ctx, db.LockFaceWatchlistParams{TenantID: tenantID, ID: id}); err != nil {
			return store.Classify(err)
		}
		used, err := q.CountRulesUsingFaceWatchlist(ctx, db.CountRulesUsingFaceWatchlistParams{TenantID: tenantID, ID: id.String()})
		if err != nil {
			return err
		}
		if used > 0 {
			return fmt.Errorf("%w: the watchlist is used by %d alarm rule(s)", store.ErrConflict, used)
		}
		if _, err := q.DeleteFaceWatchlist(ctx, db.DeleteFaceWatchlistParams{TenantID: tenantID, ID: id}); err != nil {
			return store.Classify(err)
		}
		return s.audit(ctx, q, actor, tenantID, ActionWatchlistDeleted, "face_watchlist", id, map[string]any{"watchlist_id": id})
	})
}
