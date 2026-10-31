package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func (h *Handlers) store() *store.Store {
	if h.Store != nil {
		return h.Store
	}
	if h.Events != nil && h.Events.Store != nil {
		return h.Events.Store
	}
	if h.Inv != nil && h.Inv.Store != nil {
		return h.Inv.Store
	}
	if h.Rules != nil && h.Rules.Store != nil {
		return h.Rules.Store
	}
	return nil
}

func (h *Handlers) lprCaller(ctx context.Context, explicit *uuid.UUID) (authz.Actor, uuid.UUID, error) {
	a, err := actor(ctx)
	if err != nil {
		return a, uuid.Nil, err
	}
	switch {
	case explicit != nil:
		return a, *explicit, nil
	case a.TenantID != nil:
		return a, *a.TenantID, nil
	}
	return a, uuid.Nil, &access.ForbiddenError{Permission: authz.LPRView}
}

func (h *Handlers) expireLprCache() {
	if h.Rules != nil {
		if r, ok := h.Rules.Lpr.(*rules.StoreLprResolver); ok {
			r.Expire()
		}
	}
}

func toLprWatchlist(w db.ListLprWatchlistsRow) gen.LprWatchlist {
	return gen.LprWatchlist{
		Id:          w.ID,
		TenantId:    w.TenantID,
		Name:        w.Name,
		Description: w.Description,
		Color:       w.Color,
		ItemCount:   int(w.ItemCount),
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
	}
}

func toLprWatchlistFromGet(w db.GetLprWatchlistRow) gen.LprWatchlist {
	return gen.LprWatchlist{
		Id:          w.ID,
		TenantId:    w.TenantID,
		Name:        w.Name,
		Description: w.Description,
		Color:       w.Color,
		ItemCount:   int(w.ItemCount),
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toLprWatchlistItem(item db.LprWatchlistItem) gen.LprWatchlistItem {
	return gen.LprWatchlistItem{
		Id:           item.ID,
		TenantId:     item.TenantID,
		WatchlistId:  item.WatchlistID,
		Plate:        item.Plate,
		Notes:        strPtr(item.Notes),
		VehicleBrand: strPtr(item.VehicleBrand),
		VehicleModel: strPtr(item.VehicleModel),
		VehicleColor: strPtr(item.VehicleColor),
		OwnerName:    strPtr(item.OwnerName),
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
	}
}

func (h *Handlers) ListLprWatchlists(ctx context.Context, r gen.ListLprWatchlistsRequestObject) (gen.ListLprWatchlistsResponseObject, error) {
	a, tenantID, err := h.lprCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	var out gen.ListLprWatchlists200JSONResponse
	st := h.store()
	if st == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	err = st.Tx(ctx, store.ScopeFor(a), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, a)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.LPRView, access.Tenant(tenantID)); err != nil {
			return err
		}
		rows, err := q.ListLprWatchlists(ctx, tenantID)
		if err != nil {
			return store.Classify(err)
		}
		out.Items = make([]gen.LprWatchlist, 0, len(rows))
		for _, row := range rows {
			out.Items = append(out.Items, toLprWatchlist(row))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handlers) CreateLprWatchlist(ctx context.Context, r gen.CreateLprWatchlistRequestObject) (gen.CreateLprWatchlistResponseObject, error) {
	a, tenantID, err := h.lprCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(r.Body.Name)
	if name == "" {
		return nil, &rules.ValidationError{Msg: "name cannot be empty"}
	}
	desc := ""
	if r.Body.Description != nil {
		desc = *r.Body.Description
	}
	color := "#ef4444"
	if r.Body.Color != nil && strings.TrimSpace(*r.Body.Color) != "" {
		color = strings.TrimSpace(*r.Body.Color)
	}

	var userRef *uuid.UUID
	if a.UserID != uuid.Nil {
		userRef = &a.UserID
	}

	st := h.store()
	if st == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	var created gen.LprWatchlist
	err = st.Tx(ctx, store.ScopeFor(a), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, a)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.LPRManage, access.Tenant(tenantID)); err != nil {
			return err
		}
		row, err := q.CreateLprWatchlist(ctx, db.CreateLprWatchlistParams{
			TenantID:    tenantID,
			Name:        name,
			Description: desc,
			Color:       color,
			CreatedBy:   userRef,
		})
		if err != nil {
			return store.Classify(err)
		}
		created = gen.LprWatchlist{
			Id:          row.ID,
			TenantId:    row.TenantID,
			Name:        row.Name,
			Description: row.Description,
			Color:       row.Color,
			ItemCount:   0,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	h.expireLprCache()
	return gen.CreateLprWatchlist201JSONResponse(created), nil
}

func (h *Handlers) GetLprWatchlist(ctx context.Context, r gen.GetLprWatchlistRequestObject) (gen.GetLprWatchlistResponseObject, error) {
	a, tenantID, err := h.lprCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	st := h.store()
	if st == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	var out gen.LprWatchlist
	err = st.Tx(ctx, store.ScopeFor(a), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, a)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.LPRView, access.Tenant(tenantID)); err != nil {
			return err
		}
		row, err := q.GetLprWatchlist(ctx, db.GetLprWatchlistParams{
			TenantID: tenantID,
			ID:       r.WatchlistId,
		})
		if err != nil {
			return store.Classify(err)
		}
		out = toLprWatchlistFromGet(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return gen.GetLprWatchlist200JSONResponse(out), nil
}

func (h *Handlers) UpdateLprWatchlist(ctx context.Context, r gen.UpdateLprWatchlistRequestObject) (gen.UpdateLprWatchlistResponseObject, error) {
	a, tenantID, err := h.lprCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	var userRef *uuid.UUID
	if a.UserID != uuid.Nil {
		userRef = &a.UserID
	}

	st := h.store()
	if st == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	var updated gen.LprWatchlist
	err = st.Tx(ctx, store.ScopeFor(a), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, a)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.LPRManage, access.Tenant(tenantID)); err != nil {
			return err
		}
		current, err := q.GetLprWatchlist(ctx, db.GetLprWatchlistParams{
			TenantID: tenantID,
			ID:       r.WatchlistId,
		})
		if err != nil {
			return store.Classify(err)
		}

		name := current.Name
		if r.Body.Name != nil && strings.TrimSpace(*r.Body.Name) != "" {
			name = strings.TrimSpace(*r.Body.Name)
		}
		desc := current.Description
		if r.Body.Description != nil {
			desc = *r.Body.Description
		}
		color := current.Color
		if r.Body.Color != nil && strings.TrimSpace(*r.Body.Color) != "" {
			color = strings.TrimSpace(*r.Body.Color)
		}

		row, err := q.UpdateLprWatchlist(ctx, db.UpdateLprWatchlistParams{
			TenantID:    tenantID,
			ID:          r.WatchlistId,
			Name:        name,
			Description: desc,
			Color:       color,
			UpdatedBy:   userRef,
		})
		if err != nil {
			return store.Classify(err)
		}
		updated = gen.LprWatchlist{
			Id:          row.ID,
			TenantId:    row.TenantID,
			Name:        row.Name,
			Description: row.Description,
			Color:       row.Color,
			ItemCount:   int(current.ItemCount),
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	h.expireLprCache()
	return gen.UpdateLprWatchlist200JSONResponse(updated), nil
}

func (h *Handlers) DeleteLprWatchlist(ctx context.Context, r gen.DeleteLprWatchlistRequestObject) (gen.DeleteLprWatchlistResponseObject, error) {
	a, tenantID, err := h.lprCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	st := h.store()
	if st == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	err = st.Tx(ctx, store.ScopeFor(a), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, a)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.LPRManage, access.Tenant(tenantID)); err != nil {
			return err
		}
		if _, err := q.GetLprWatchlist(ctx, db.GetLprWatchlistParams{TenantID: tenantID, ID: r.WatchlistId}); err != nil {
			return store.Classify(err)
		}
		if _, err := q.LockLprWatchlist(ctx, db.LockLprWatchlistParams{TenantID: tenantID, ID: r.WatchlistId}); err != nil {
			return store.Classify(err)
		}
		used, err := q.CountRulesUsingLprWatchlist(ctx, db.CountRulesUsingLprWatchlistParams{TenantID: tenantID, ID: r.WatchlistId.String()})
		if err != nil {
			return store.Classify(err)
		}
		if used > 0 {
			return fmt.Errorf("%w: watchlist is used by %d alarm rule(s)", store.ErrConflict, used)
		}
		n, err := q.DeleteLprWatchlist(ctx, db.DeleteLprWatchlistParams{
			TenantID: tenantID,
			ID:       r.WatchlistId,
		})
		if err != nil {
			return store.Classify(err)
		}
		if n == 0 {
			return store.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	h.expireLprCache()
	return gen.DeleteLprWatchlist204Response{}, nil
}

func (h *Handlers) ListLprWatchlistItems(ctx context.Context, r gen.ListLprWatchlistItemsRequestObject) (gen.ListLprWatchlistItemsResponseObject, error) {
	a, tenantID, err := h.lprCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	st := h.store()
	if st == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	var out gen.ListLprWatchlistItems200JSONResponse
	err = st.Tx(ctx, store.ScopeFor(a), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, a)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.LPRView, access.Tenant(tenantID)); err != nil {
			return err
		}
		if _, err := q.GetLprWatchlist(ctx, db.GetLprWatchlistParams{TenantID: tenantID, ID: r.WatchlistId}); err != nil {
			return store.Classify(err)
		}
		items, err := q.ListLprWatchlistItems(ctx, db.ListLprWatchlistItemsParams{
			TenantID:    tenantID,
			WatchlistID: r.WatchlistId,
		})
		if err != nil {
			return store.Classify(err)
		}
		out.Items = make([]gen.LprWatchlistItem, 0, len(items))
		for _, item := range items {
			out.Items = append(out.Items, toLprWatchlistItem(item))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handlers) AddLprWatchlistItem(ctx context.Context, r gen.AddLprWatchlistItemRequestObject) (gen.AddLprWatchlistItemResponseObject, error) {
	a, tenantID, err := h.lprCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	plate := strings.ToUpper(strings.TrimSpace(r.Body.Plate))
	if len(plate) < 2 {
		return nil, &rules.ValidationError{Msg: "plate must be at least 2 characters"}
	}
	notes := ""
	if r.Body.Notes != nil {
		notes = *r.Body.Notes
	}
	brand := ""
	if r.Body.VehicleBrand != nil {
		brand = *r.Body.VehicleBrand
	}
	model := ""
	if r.Body.VehicleModel != nil {
		model = *r.Body.VehicleModel
	}
	color := ""
	if r.Body.VehicleColor != nil {
		color = *r.Body.VehicleColor
	}
	owner := ""
	if r.Body.OwnerName != nil {
		owner = *r.Body.OwnerName
	}

	st := h.store()
	if st == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	var created gen.LprWatchlistItem
	err = st.Tx(ctx, store.ScopeFor(a), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, a)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.LPRManage, access.Tenant(tenantID)); err != nil {
			return err
		}
		if _, err := q.GetLprWatchlist(ctx, db.GetLprWatchlistParams{TenantID: tenantID, ID: r.WatchlistId}); err != nil {
			return store.Classify(err)
		}
		item, err := q.AddLprWatchlistItem(ctx, db.AddLprWatchlistItemParams{
			TenantID:     tenantID,
			WatchlistID:  r.WatchlistId,
			Plate:        plate,
			Notes:        notes,
			VehicleBrand: brand,
			VehicleModel: model,
			VehicleColor: color,
			OwnerName:    owner,
		})
		if err != nil {
			return store.Classify(err)
		}
		created = toLprWatchlistItem(item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	h.expireLprCache()
	return gen.AddLprWatchlistItem201JSONResponse(created), nil
}

func (h *Handlers) RemoveLprWatchlistItem(ctx context.Context, r gen.RemoveLprWatchlistItemRequestObject) (gen.RemoveLprWatchlistItemResponseObject, error) {
	a, tenantID, err := h.lprCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	st := h.store()
	if st == nil {
		return nil, fmt.Errorf("storage not configured")
	}
	err = st.Tx(ctx, store.ScopeFor(a), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, a)
		if err != nil {
			return err
		}
		if err := chk.Require(authz.LPRManage, access.Tenant(tenantID)); err != nil {
			return err
		}
		n, err := q.RemoveLprWatchlistItem(ctx, db.RemoveLprWatchlistItemParams{
			TenantID:    tenantID,
			WatchlistID: r.WatchlistId,
			ID:          r.ItemId,
		})
		if err != nil {
			return store.Classify(err)
		}
		if n == 0 {
			return store.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	h.expireLprCache()
	return gen.RemoveLprWatchlistItem204Response{}, nil
}
