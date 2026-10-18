package api

import (
	"context"
	"errors"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

var errFaceWatchlistsPending = errors.New("face watchlist handlers are added in the next commit")

func (h *Handlers) ListFaceWatchlists(context.Context, gen.ListFaceWatchlistsRequestObject) (gen.ListFaceWatchlistsResponseObject, error) {
	return nil, errFaceWatchlistsPending
}

func (h *Handlers) CreateFaceWatchlist(context.Context, gen.CreateFaceWatchlistRequestObject) (gen.CreateFaceWatchlistResponseObject, error) {
	return nil, errFaceWatchlistsPending
}

func (h *Handlers) GetFaceWatchlist(context.Context, gen.GetFaceWatchlistRequestObject) (gen.GetFaceWatchlistResponseObject, error) {
	return nil, errFaceWatchlistsPending
}

func (h *Handlers) UpdateFaceWatchlist(context.Context, gen.UpdateFaceWatchlistRequestObject) (gen.UpdateFaceWatchlistResponseObject, error) {
	return nil, errFaceWatchlistsPending
}

func (h *Handlers) DeleteFaceWatchlist(context.Context, gen.DeleteFaceWatchlistRequestObject) (gen.DeleteFaceWatchlistResponseObject, error) {
	return nil, errFaceWatchlistsPending
}
