package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
)

func toExportJob(j media.ExportJob) gen.ExportJob {
	out := gen.ExportJob{
		Id:               j.ID,
		TenantId:         j.TenantID,
		SiteId:           j.SiteID,
		RequestedBy:      j.RequestedBy,
		RequestedByName:  j.Requester,
		Name:             j.Name,
		StartTime:        j.Start,
		EndTime:          j.End,
		Status:           gen.ExportJobStatus(j.Status),
		Progress:         j.Progress,
		Error:            j.Error,
		TotalBytes:       j.TotalBytes,
		TransferredBytes: j.TransferredBytes,
		SpeedBps:         j.SpeedBps,
		EtaSeconds:       j.ETASeconds,
		CameraCount:      j.CameraCount,
		Protected:        j.Protected,
		ExpiresAt:        j.ExpiresAt,
		CreatedAt:        j.CreatedAt,
		UpdatedAt:        j.UpdatedAt,
	}
	if j.LocalPath != "" {
		out.LocalPath = &j.LocalPath
	}
	if len(j.Manifest) > 0 {
		out.Manifest = &j.Manifest
	}
	if len(j.Items) > 0 {
		items := make([]gen.ExportJobItem, 0, len(j.Items))
		for _, it := range j.Items {
			item := gen.ExportJobItem{
				Id:               it.ID,
				JobId:            it.JobID,
				TenantId:         it.TenantID,
				CameraId:         it.CameraID,
				CameraName:       it.CameraName,
				ServerId:         it.ServerID,
				ServerName:       it.ServerName,
				Status:           gen.ExportJobItemStatus(it.Status),
				Progress:         it.Progress,
				Error:            it.Error,
				TotalBytes:       it.TotalBytes,
				TransferredBytes: it.TransferredBytes,
				CreatedAt:        it.CreatedAt,
				UpdatedAt:        it.UpdatedAt,
			}
			if it.RemoteExportID != "" {
				item.RemoteExportId = &it.RemoteExportID
			}
			if it.RemotePath != "" {
				item.RemotePath = &it.RemotePath
			}
			if it.LocalPath != "" {
				item.LocalPath = &it.LocalPath
			}
			if it.SHA256Hash != "" {
				item.Sha256Hash = &it.SHA256Hash
			}
			items = append(items, item)
		}
		out.Items = &items
	}
	return out
}

func (h *Handlers) ListExportJobs(ctx context.Context, _ gen.ListExportJobsRequestObject) (gen.ListExportJobsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	jobs, err := h.Media.ListExportJobs(ctx, a)
	if err != nil {
		return nil, err
	}
	out := gen.ListExportJobs200JSONResponse{Items: make([]gen.ExportJob, 0, len(jobs))}
	for _, j := range jobs {
		out.Items = append(out.Items, toExportJob(j))
	}
	return out, nil
}

func (h *Handlers) CreateExportJob(ctx context.Context, r gen.CreateExportJobRequestObject) (gen.CreateExportJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	if b == nil {
		return nil, &inventory.ValidationError{Msg: "request body is required"}
	}
	job, err := h.Media.CreateExportJob(ctx, a, media.CreateExportJobInput{
		CameraIDs: b.CameraIds,
		Start:     b.StartTime,
		End:       b.EndTime,
		Name:      deref(b.Name),
		Protected: deref(b.Protected),
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateExportJob201JSONResponse(toExportJob(job)), nil
}

func (h *Handlers) GetExportJob(ctx context.Context, r gen.GetExportJobRequestObject) (gen.GetExportJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	job, err := h.Media.GetExportJob(ctx, a, r.JobId)
	if err != nil {
		return nil, err
	}
	return gen.GetExportJob200JSONResponse(toExportJob(job)), nil
}

func (h *Handlers) DeleteExportJob(ctx context.Context, r gen.DeleteExportJobRequestObject) (gen.DeleteExportJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Media.DeleteExportJob(ctx, a, r.JobId); err != nil {
		return nil, err
	}
	return gen.DeleteExportJob204Response{}, nil
}

func (h *Handlers) CancelExportJob(ctx context.Context, r gen.CancelExportJobRequestObject) (gen.CancelExportJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	job, err := h.Media.CancelExportJob(ctx, a, r.JobId)
	if err != nil {
		return nil, err
	}
	return gen.CancelExportJob200JSONResponse(toExportJob(job)), nil
}

func (h *Handlers) RetryExportJob(ctx context.Context, r gen.RetryExportJobRequestObject) (gen.RetryExportJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	job, err := h.Media.RetryExportJob(ctx, a, r.JobId)
	if err != nil {
		return nil, err
	}
	return gen.RetryExportJob200JSONResponse(toExportJob(job)), nil
}

func (h *Handlers) CreateExportShare(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r.Context())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "bad_request", "invalid job id")
		return
	}
	var in media.CreateExportShareInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	share, err := h.Media.CreateExportShare(r.Context(), a, id, in)
	if err != nil {
		status, code, msg := statusFor(err)
		writeError(w, r, status, code, msg)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(share)
}

func (h *Handlers) ListExportShares(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r.Context())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "bad_request", "invalid job id")
		return
	}
	shares, err := h.Media.ListExportShares(r.Context(), a, id)
	if err != nil {
		status, code, msg := statusFor(err)
		writeError(w, r, status, code, msg)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(shares)
}

func (h *Handlers) RevokeExportShare(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r.Context())
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	shareID, err := uuid.Parse(chi.URLParam(r, "shareId"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "bad_request", "invalid share id")
		return
	}
	if err := h.Media.RevokeExportShare(r.Context(), a, shareID); err != nil {
		status, code, msg := statusFor(err)
		writeError(w, r, status, code, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
