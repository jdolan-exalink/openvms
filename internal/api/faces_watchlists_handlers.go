package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/faces"
)

func toFaceWatchlist(w faces.Watchlist) gen.FaceWatchlist {
	return gen.FaceWatchlist{Id: w.ID, TenantId: w.TenantID, Name: w.Name, Description: w.Description,
		PersonIds: w.PersonIDs, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
}

func (h *Handlers) ListFaceWatchlists(ctx context.Context, r gen.ListFaceWatchlistsRequestObject) (gen.ListFaceWatchlistsResponseObject, error) {
	a, tenant, err := h.faceCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	list, err := h.Faces.ListWatchlists(ctx, a, tenant)
	if err != nil {
		return nil, err
	}
	out := gen.ListFaceWatchlists200JSONResponse{Items: make([]gen.FaceWatchlist, 0, len(list))}
	for _, w := range list {
		out.Items = append(out.Items, toFaceWatchlist(w))
	}
	return out, nil
}

func (h *Handlers) CreateFaceWatchlist(ctx context.Context, r gen.CreateFaceWatchlistRequestObject) (gen.CreateFaceWatchlistResponseObject, error) {
	a, tenant, err := h.faceCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	in := faces.WatchlistInput{}
	if b := r.Body; b != nil {
		in.Name = b.Name
		if b.Description != nil {
			in.Description = *b.Description
		}
		if b.PersonIds != nil {
			in.PersonIDs = *b.PersonIds
		}
	}
	w, err := h.Faces.CreateWatchlist(ctx, a, tenant, in)
	if err != nil {
		return nil, err
	}
	return gen.CreateFaceWatchlist201JSONResponse(toFaceWatchlist(w)), nil
}

func (h *Handlers) GetFaceWatchlist(ctx context.Context, r gen.GetFaceWatchlistRequestObject) (gen.GetFaceWatchlistResponseObject, error) {
	a, tenant, err := h.faceWatchlistCaller(ctx, r.Params.TenantId, r.WatchlistId)
	if err != nil {
		return nil, err
	}
	w, err := h.Faces.GetWatchlist(ctx, a, tenant, r.WatchlistId)
	if err != nil {
		return nil, err
	}
	return gen.GetFaceWatchlist200JSONResponse(toFaceWatchlist(w)), nil
}

func (h *Handlers) UpdateFaceWatchlist(ctx context.Context, r gen.UpdateFaceWatchlistRequestObject) (gen.UpdateFaceWatchlistResponseObject, error) {
	a, tenant, err := h.faceWatchlistCaller(ctx, r.Params.TenantId, r.WatchlistId)
	if err != nil {
		return nil, err
	}
	in := faces.WatchlistUpdate{}
	if b := r.Body; b != nil {
		in.Name, in.Description = b.Name, b.Description
		if b.PersonIds != nil {
			ids := append([]uuid.UUID{}, *b.PersonIds...)
			in.PersonIDs = &ids
		}
	}
	w, err := h.Faces.UpdateWatchlist(ctx, a, tenant, r.WatchlistId, in)
	if err != nil {
		return nil, err
	}
	return gen.UpdateFaceWatchlist200JSONResponse(toFaceWatchlist(w)), nil
}

func (h *Handlers) DeleteFaceWatchlist(ctx context.Context, r gen.DeleteFaceWatchlistRequestObject) (gen.DeleteFaceWatchlistResponseObject, error) {
	a, tenant, err := h.faceWatchlistCaller(ctx, r.Params.TenantId, r.WatchlistId)
	if err != nil {
		return nil, err
	}
	if err := h.Faces.DeleteWatchlist(ctx, a, tenant, r.WatchlistId); err != nil {
		return nil, err
	}
	return gen.DeleteFaceWatchlist204Response{}, nil
}
