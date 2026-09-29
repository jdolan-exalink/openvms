package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/media"
)

func toLayout(l media.Layout) gen.ViewLayout {
	out := gen.ViewLayout{Columns: l.Columns, Cells: make([]gen.ViewCell, 0, len(l.Cells))}
	for _, c := range l.Cells {
		if c == nil {
			out.Cells = append(out.Cells, gen.ViewCell{})
			continue
		}
		id := c.CameraID
		cell := gen.ViewCell{CameraId: &id}
		if c.Quality != "" {
			q := gen.ViewCellQuality(c.Quality)
			cell.Quality = &q
		}
		out.Cells = append(out.Cells, cell)
	}
	return out
}

func fromLayout(l gen.ViewLayout) media.Layout {
	out := media.Layout{Columns: l.Columns, Cells: make([]*media.Cell, 0, len(l.Cells))}
	for _, c := range l.Cells {
		if c.CameraId == nil {
			out.Cells = append(out.Cells, nil)
			continue
		}
		cell := &media.Cell{CameraID: *c.CameraId}
		if c.Quality != nil {
			cell.Quality = string(*c.Quality)
		}
		out.Cells = append(out.Cells, cell)
	}
	return out
}

func toView(v media.View) gen.View {
	return gen.View{
		Id: v.ID, TenantId: v.TenantID, OwnerId: v.OwnerID, OwnerName: v.OwnerName, Name: v.Name, Shared: v.Shared,
		Layout: toLayout(v.Layout), Editable: v.Editable, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func viewInput(b *gen.ViewInput) media.ViewInput {
	return media.ViewInput{TenantID: b.TenantId, Name: b.Name, Shared: deref(b.Shared), Layout: fromLayout(b.Layout)}
}

func (h *Handlers) ListViews(ctx context.Context, _ gen.ListViewsRequestObject) (gen.ListViewsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	vs, err := h.Media.ListViews(ctx, a)
	if err != nil {
		return nil, err
	}
	out := gen.ListViews200JSONResponse{Items: make([]gen.View, 0, len(vs))}
	for _, v := range vs {
		out.Items = append(out.Items, toView(v))
	}
	return out, nil
}

func (h *Handlers) CreateView(ctx context.Context, r gen.CreateViewRequestObject) (gen.CreateViewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.Media.CreateView(ctx, a, viewInput(r.Body))
	if err != nil {
		return nil, err
	}
	return gen.CreateView201JSONResponse(toView(v)), nil
}

func (h *Handlers) GetView(ctx context.Context, r gen.GetViewRequestObject) (gen.GetViewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.Media.GetView(ctx, a, r.ViewId)
	if err != nil {
		return nil, err
	}
	return gen.GetView200JSONResponse(toView(v)), nil
}

func (h *Handlers) ReplaceView(ctx context.Context, r gen.ReplaceViewRequestObject) (gen.ReplaceViewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.Media.ReplaceView(ctx, a, r.ViewId, viewInput(r.Body))
	if err != nil {
		return nil, err
	}
	return gen.ReplaceView200JSONResponse(toView(v)), nil
}

func (h *Handlers) DeleteView(ctx context.Context, r gen.DeleteViewRequestObject) (gen.DeleteViewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Media.DeleteView(ctx, a, r.ViewId); err != nil {
		return nil, err
	}
	return gen.DeleteView204Response{}, nil
}

func (h *Handlers) ListCameraRecordings(ctx context.Context, r gen.ListCameraRecordingsRequestObject) (gen.ListCameraRecordingsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	spans, err := h.Media.Recordings(ctx, a, r.CameraId, r.Params.From, r.Params.To)
	if err != nil {
		return nil, err
	}
	out := gen.ListCameraRecordings200JSONResponse{Items: make([]gen.RecordingSpan, 0, len(spans))}
	for _, s := range spans {
		out.Items = append(out.Items, gen.RecordingSpan{StartTime: s.Start, EndTime: s.End, Motion: s.Motion, Objects: s.Objects})
	}
	return out, nil
}

func toExport(e media.Export) gen.Export {
	return gen.Export{
		Id: e.ID, TenantId: e.TenantID, SiteId: e.SiteID, ServerId: e.ServerID, CameraId: e.CameraID, CameraName: e.CameraName,
		RequestedBy: e.RequestedBy, RequestedByName: e.Requester, Name: e.Name, StartTime: e.Start, EndTime: e.End,
		Status: gen.ExportStatus(e.Status), Progress: e.Progress, Error: e.Error, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func (h *Handlers) ListExports(ctx context.Context, _ gen.ListExportsRequestObject) (gen.ListExportsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	xs, err := h.Media.ListExports(ctx, a)
	if err != nil {
		return nil, err
	}
	out := gen.ListExports200JSONResponse{Items: make([]gen.Export, 0, len(xs))}
	for _, x := range xs {
		out.Items = append(out.Items, toExport(x))
	}
	return out, nil
}

func (h *Handlers) CreateExport(ctx context.Context, r gen.CreateExportRequestObject) (gen.CreateExportResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	x, err := h.Media.CreateExport(ctx, a, media.ExportInput{CameraID: b.CameraId, Start: b.StartTime, End: b.EndTime, Name: deref(b.Name)})
	if err != nil {
		return nil, err
	}
	return gen.CreateExport201JSONResponse(toExport(x)), nil
}

func (h *Handlers) GetExport(ctx context.Context, r gen.GetExportRequestObject) (gen.GetExportResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	x, err := h.Media.GetExport(ctx, a, r.ExportId)
	if err != nil {
		return nil, err
	}
	return gen.GetExport200JSONResponse(toExport(x)), nil
}

func (h *Handlers) DeleteExport(ctx context.Context, r gen.DeleteExportRequestObject) (gen.DeleteExportResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Media.DeleteExport(ctx, a, r.ExportId); err != nil {
		return nil, err
	}
	return gen.DeleteExport204Response{}, nil
}
