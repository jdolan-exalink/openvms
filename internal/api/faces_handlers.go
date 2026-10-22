package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/faces"
)

func toFacePerson(p faces.Person) gen.FacePerson {
	out := gen.FacePerson{
		Id: p.ID, TenantId: p.TenantID, DisplayName: p.DisplayName, FrigateName: p.FrigateName, Notes: p.Notes,
		ImageCount: p.ImageCount, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		Sync: gen.FaceSyncSummary{Total: p.Sync.Total, Synced: p.Sync.Synced, Pending: p.Sync.Pending, Failed: p.Sync.Failed,
			Conflict: p.Sync.Conflict, Disabled: p.Sync.Disabled},
	}
	out.FirstImageId = p.FirstImageID
	if lr := p.LastRecognition; lr != nil {
		out.LastRecognition = &gen.FaceLastRecognition{EventId: lr.EventID, StartTime: lr.StartTime, HasThumbnail: lr.HasThumbnail}
	}
	return out
}

func toFaceImage(i faces.Image) gen.FaceImage {
	return gen.FaceImage{Id: i.ID, PersonId: i.PersonID, ContentType: i.ContentType, SizeBytes: i.SizeBytes,
		Width: i.Width, Height: i.Height, CreatedAt: i.CreatedAt}
}

// faceImageResponse serves biometric image bytes that must never be cached by a shared cache.
type faceImageResponse struct {
	data        []byte
	contentType string
}

func (r faceImageResponse) write(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", r.contentType)
	w.Header().Set("Content-Length", fmt.Sprint(len(r.data)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, bytes.NewReader(r.data))
	return err
}

type getFaceImageResponse struct{ faceImageResponse }

func (r getFaceImageResponse) VisitGetFaceImageResponse(w http.ResponseWriter) error {
	return r.write(w)
}

type getFaceTrainImageResponse struct{ faceImageResponse }

func (r getFaceTrainImageResponse) VisitGetFaceTrainImageResponse(w http.ResponseWriter) error {
	return r.write(w)
}

func (h *Handlers) ListFacePersons(ctx context.Context, r gen.ListFacePersonsRequestObject) (gen.ListFacePersonsResponseObject, error) {
	a, tenant, err := h.faceCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	list, err := h.Faces.ListPersons(ctx, a, tenant)
	if err != nil {
		return nil, err
	}
	out := gen.ListFacePersons200JSONResponse{Items: make([]gen.FacePerson, 0, len(list))}
	for _, p := range list {
		out.Items = append(out.Items, toFacePerson(p))
	}
	return out, nil
}

func (h *Handlers) ListPendingFaceTombstones(ctx context.Context, r gen.ListPendingFaceTombstonesRequestObject) (gen.ListPendingFaceTombstonesResponseObject, error) {
	a, tenant, err := h.faceCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	list, err := h.Faces.ListPendingTombstones(ctx, a, tenant)
	if err != nil {
		return nil, err
	}
	out := gen.ListPendingFaceTombstones200JSONResponse{Items: make([]gen.FaceTombstone, 0, len(list))}
	for _, p := range list {
		out.Items = append(out.Items, gen.FaceTombstone{Id: p.ID, DisplayName: p.DisplayName, DeletedAt: p.DeletedAt, PendingServerCount: p.PendingServerCount})
	}
	return out, nil
}

func (h *Handlers) CreateFacePerson(ctx context.Context, r gen.CreateFacePersonRequestObject) (gen.CreateFacePersonResponseObject, error) {
	a, tenant, err := h.faceCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	in := faces.PersonInput{}
	if r.Body != nil {
		in.DisplayName = r.Body.DisplayName
		if r.Body.Notes != nil {
			in.Notes = *r.Body.Notes
		}
	}
	p, err := h.Faces.CreatePerson(ctx, a, tenant, in)
	if err != nil {
		return nil, err
	}
	return gen.CreateFacePerson201JSONResponse(toFacePerson(p)), nil
}

func (h *Handlers) GetFacePerson(ctx context.Context, r gen.GetFacePersonRequestObject) (gen.GetFacePersonResponseObject, error) {
	a, tenant, err := h.facePersonCaller(ctx, r.Params.TenantId, r.PersonId)
	if err != nil {
		return nil, err
	}
	p, err := h.Faces.GetPerson(ctx, a, tenant, r.PersonId)
	if err != nil {
		return nil, err
	}
	return gen.GetFacePerson200JSONResponse(toFacePerson(p)), nil
}

func (h *Handlers) UpdateFacePerson(ctx context.Context, r gen.UpdateFacePersonRequestObject) (gen.UpdateFacePersonResponseObject, error) {
	a, tenant, err := h.facePersonCaller(ctx, r.Params.TenantId, r.PersonId)
	if err != nil {
		return nil, err
	}
	in := faces.PersonUpdate{}
	if r.Body != nil {
		in.DisplayName, in.Notes = r.Body.DisplayName, r.Body.Notes
	}
	p, err := h.Faces.UpdatePerson(ctx, a, tenant, r.PersonId, in)
	if err != nil {
		return nil, err
	}
	return gen.UpdateFacePerson200JSONResponse(toFacePerson(p)), nil
}

func (h *Handlers) DeleteFacePerson(ctx context.Context, r gen.DeleteFacePersonRequestObject) (gen.DeleteFacePersonResponseObject, error) {
	a, tenant, err := h.facePersonCaller(ctx, r.Params.TenantId, r.PersonId)
	if err != nil {
		return nil, err
	}
	if err := h.Faces.DeletePerson(ctx, a, tenant, r.PersonId); err != nil {
		return nil, err
	}
	return gen.DeleteFacePerson204Response{}, nil
}

func (h *Handlers) ForcePurgeFaceTombstone(ctx context.Context, r gen.ForcePurgeFaceTombstoneRequestObject) (gen.ForcePurgeFaceTombstoneResponseObject, error) {
	// facePersonCaller intentionally cannot resolve tombstones because ordinary person detail hides
	// them. Resolve tenant scope first, then let the service return the same 404 for active/missing IDs.
	a, tenant, err := h.faceCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	if err := h.Faces.ForcePurgeTombstone(ctx, a, tenant, r.PersonId); err != nil {
		return nil, err
	}
	return gen.ForcePurgeFaceTombstone204Response{}, nil
}

func (h *Handlers) ListFaceImages(ctx context.Context, r gen.ListFaceImagesRequestObject) (gen.ListFaceImagesResponseObject, error) {
	a, tenant, err := h.facePersonCaller(ctx, r.Params.TenantId, r.PersonId)
	if err != nil {
		return nil, err
	}
	imgs, err := h.Faces.ListImages(ctx, a, tenant, r.PersonId)
	if err != nil {
		return nil, err
	}
	out := gen.ListFaceImages200JSONResponse{Items: make([]gen.FaceImage, 0, len(imgs))}
	for _, i := range imgs {
		out.Items = append(out.Items, toFaceImage(i))
	}
	return out, nil
}

const maxMultipartParts = 4

// readUploadedImage returns the bytes and content type of the `file` part of a multipart body,
// reading at most MaxImageBytes+1 so an oversized upload is rejected without buffering it.
func readUploadedImage(next func() (partInfo, error)) ([]byte, string, error) {
	for i := 0; i < maxMultipartParts; i++ {
		p, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", &faces.ValidationError{Msg: "the upload is not a valid multipart form"}
		}
		if p.name != "file" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(p.body, faces.MaxImageBytes+1))
		if err != nil {
			return nil, "", &faces.ValidationError{Msg: "the upload could not be read"}
		}
		ct := p.contentType
		if ct == "" || strings.HasPrefix(ct, "application/octet-stream") {
			ct = http.DetectContentType(data)
		}
		return data, ct, nil
	}
	return nil, "", &faces.ValidationError{Msg: "a multipart part named file is required"}
}

type partInfo struct {
	name, contentType string
	body              io.Reader
}

func (h *Handlers) UploadFaceImage(ctx context.Context, r gen.UploadFaceImageRequestObject) (gen.UploadFaceImageResponseObject, error) {
	a, tenant, err := h.facePersonCaller(ctx, r.Params.TenantId, r.PersonId)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &faces.ValidationError{Msg: "a multipart form with a file part is required"}
	}
	data, ct, err := readUploadedImage(func() (partInfo, error) {
		p, err := r.Body.NextPart()
		if err != nil {
			return partInfo{}, err
		}
		return partInfo{name: p.FormName(), contentType: p.Header.Get("Content-Type"), body: p}, nil
	})
	if err != nil {
		return nil, err
	}
	img, err := h.Faces.AddImage(ctx, a, tenant, r.PersonId, data, ct)
	if err != nil {
		return nil, err
	}
	return gen.UploadFaceImage201JSONResponse(toFaceImage(img)), nil
}

func (h *Handlers) GetFaceImage(ctx context.Context, r gen.GetFaceImageRequestObject) (gen.GetFaceImageResponseObject, error) {
	a, tenant, err := h.facePersonCaller(ctx, r.Params.TenantId, r.PersonId)
	if err != nil {
		return nil, err
	}
	data, ct, err := h.Faces.ReadImage(ctx, a, tenant, r.PersonId, r.ImageId)
	if err != nil {
		return nil, err
	}
	return getFaceImageResponse{faceImageResponse{data: data, contentType: ct}}, nil
}

func (h *Handlers) DeleteFaceImage(ctx context.Context, r gen.DeleteFaceImageRequestObject) (gen.DeleteFaceImageResponseObject, error) {
	a, tenant, err := h.facePersonCaller(ctx, r.Params.TenantId, r.PersonId)
	if err != nil {
		return nil, err
	}
	if err := h.Faces.RemoveImage(ctx, a, tenant, r.PersonId, r.ImageId); err != nil {
		return nil, err
	}
	return gen.DeleteFaceImage204Response{}, nil
}

func (h *Handlers) ListFaceSyncStatus(ctx context.Context, r gen.ListFaceSyncStatusRequestObject) (gen.ListFaceSyncStatusResponseObject, error) {
	a, tenant, err := h.facePersonCaller(ctx, r.Params.TenantId, r.PersonId)
	if err != nil {
		return nil, err
	}
	list, err := h.Faces.ListSyncStatus(ctx, a, tenant, r.PersonId)
	if err != nil {
		return nil, err
	}
	out := gen.ListFaceSyncStatus200JSONResponse{Items: make([]gen.FaceSyncStatus, 0, len(list))}
	for _, s := range list {
		item := gen.FaceSyncStatus{ServerId: s.ServerID, ServerName: s.ServerName, Status: gen.FaceSyncStatusStatus(s.Status),
			Owned: s.Owned, DesiredRevision: s.DesiredRevision, SyncedRevision: s.SyncedRevision, LastSyncedAt: s.LastSyncedAt, UpdatedAt: s.UpdatedAt}
		if s.LastError != "" {
			msg := s.LastError
			item.LastError = &msg
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func (h *Handlers) RetryFaceSync(ctx context.Context, r gen.RetryFaceSyncRequestObject) (gen.RetryFaceSyncResponseObject, error) {
	a, tenant, err := h.facePersonCaller(ctx, r.Params.TenantId, r.PersonId)
	if err != nil {
		return nil, err
	}
	n, err := h.Faces.RetrySync(ctx, a, tenant, r.PersonId)
	if err != nil {
		return nil, err
	}
	return gen.RetryFaceSync200JSONResponse{Requeued: n}, nil
}

func (h *Handlers) ListFaceRecognitions(ctx context.Context, r gen.ListFaceRecognitionsRequestObject) (gen.ListFaceRecognitionsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p := r.Params
	page, err := h.Faces.ListRecognitions(ctx, a, faces.RecognitionFilter{
		TenantID: p.TenantId, PersonIDs: uuids(p.PersonId), SiteIDs: uuids(p.SiteId), ServerIDs: uuids(p.ServerId),
		CameraGroupIDs: uuids(p.CameraGroupId), CameraIDs: uuids(p.CameraId), From: p.From, To: p.To,
		Cursor: deref(p.Cursor), Limit: deref(p.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := gen.ListFaceRecognitions200JSONResponse{Items: make([]gen.FaceRecognition, 0, len(page.Items)), NextCursor: nextCursor(page.Next)}
	for _, x := range page.Items {
		out.Items = append(out.Items, gen.FaceRecognition{
			EventId: x.EventID, PersonId: x.PersonID, PersonName: x.PersonName, CameraId: x.CameraID, CameraName: x.CameraName,
			SiteId: x.SiteID, SiteName: x.SiteName, ServerName: x.ServerName, StartTime: x.StartTime, EndTime: x.EndTime,
			HasThumbnail: x.HasThumbnail, HasSnapshot: x.HasSnapshot,
		})
	}
	return out, nil
}

func (h *Handlers) ListFaceTrainAttempts(ctx context.Context, r gen.ListFaceTrainAttemptsRequestObject) (gen.ListFaceTrainAttemptsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.Faces.ListTrainAttempts(ctx, a, faces.TrainFilter{
		TenantID: r.Params.TenantId, SiteIDs: uuids(r.Params.SiteId), ServerIDs: uuids(r.Params.ServerId)})
	if err != nil {
		return nil, err
	}
	out := gen.ListFaceTrainAttempts200JSONResponse{
		Items:   make([]gen.FaceTrainAttempt, 0, len(list.Attempts)),
		Servers: make([]gen.FaceTrainServer, 0, len(list.Servers)),
	}
	for _, t := range list.Attempts {
		item := gen.FaceTrainAttempt{ServerId: t.ServerID, ServerName: t.ServerName, File: t.File, SeenAt: t.SeenAt, Score: t.Score}
		if t.Label != "" {
			label := t.Label
			item.Label = &label
		}
		out.Items = append(out.Items, item)
	}
	for _, s := range list.Servers {
		item := gen.FaceTrainServer{ServerId: s.ServerID, ServerName: s.ServerName}
		if s.Error != "" {
			msg := s.Error
			item.Error = &msg
		}
		out.Servers = append(out.Servers, item)
	}
	return out, nil
}

func (h *Handlers) GetFaceTrainImage(ctx context.Context, r gen.GetFaceTrainImageRequestObject) (gen.GetFaceTrainImageResponseObject, error) {
	a, tenant, err := h.faceServerCaller(ctx, r.Params.TenantId, r.ServerId)
	if err != nil {
		return nil, err
	}
	data, ct, err := h.Faces.TrainAttemptImage(ctx, a, tenant, r.ServerId, r.Params.File)
	if err != nil {
		return nil, err
	}
	return getFaceTrainImageResponse{faceImageResponse{data: data, contentType: ct}}, nil
}

func (h *Handlers) AssignFaceTrainAttempt(ctx context.Context, r gen.AssignFaceTrainAttemptRequestObject) (gen.AssignFaceTrainAttemptResponseObject, error) {
	if r.Body == nil {
		return nil, &faces.ValidationError{Msg: "a request body is required"}
	}
	a, tenant, err := h.faceServerCaller(ctx, r.Params.TenantId, r.Body.ServerId)
	if err != nil {
		return nil, err
	}
	in := faces.AssignInput{ServerID: r.Body.ServerId, File: r.Body.File, PersonID: r.Body.PersonId}
	if r.Body.NewPersonDisplayName != nil {
		in.NewPersonName = *r.Body.NewPersonDisplayName
	}
	res, err := h.Faces.AssignTrainAttempt(ctx, a, tenant, in)
	if err != nil {
		return nil, err
	}
	return gen.AssignFaceTrainAttempt201JSONResponse{
		Person: toFacePerson(res.Person), Image: toFaceImage(res.Image), AttemptRemoved: res.AttemptRemoved,
	}, nil
}

// faceCaller resolves the actor and the tenant of a faces route that names no object (lists and
// creates): the explicit tenant_id, else the member's own tenant, else, for a platform
// administrator, the only organization that exists. A member naming another tenant is answered
// 404 by the service.
func (h *Handlers) faceCaller(ctx context.Context, explicit *uuid.UUID) (authz.Actor, uuid.UUID, error) {
	return h.faceObjectCaller(ctx, explicit, h.Faces.SoleTenant)
}

// faceObjectCaller is faceCaller for a route that references an object: a platform administrator
// without tenant_id acts on the tenant the object (resolve) belongs to. Members and explicit
// tenants skip the lookup and are authorized by the service as always, so another tenant's
// object stays 404.
func (h *Handlers) faceObjectCaller(ctx context.Context, explicit *uuid.UUID, resolve func(context.Context, authz.Actor) (uuid.UUID, error)) (authz.Actor, uuid.UUID, error) {
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
	tenant, err := resolve(ctx, a)
	return a, tenant, err
}

func (h *Handlers) facePersonCaller(ctx context.Context, explicit *uuid.UUID, personID uuid.UUID) (authz.Actor, uuid.UUID, error) {
	return h.faceObjectCaller(ctx, explicit, func(ctx context.Context, a authz.Actor) (uuid.UUID, error) {
		return h.Faces.PersonTenant(ctx, a, personID)
	})
}

func (h *Handlers) faceWatchlistCaller(ctx context.Context, explicit *uuid.UUID, watchlistID uuid.UUID) (authz.Actor, uuid.UUID, error) {
	return h.faceObjectCaller(ctx, explicit, func(ctx context.Context, a authz.Actor) (uuid.UUID, error) {
		return h.Faces.WatchlistTenant(ctx, a, watchlistID)
	})
}

func (h *Handlers) faceServerCaller(ctx context.Context, explicit *uuid.UUID, serverID uuid.UUID) (authz.Actor, uuid.UUID, error) {
	return h.faceObjectCaller(ctx, explicit, func(ctx context.Context, a authz.Actor) (uuid.UUID, error) {
		return h.Faces.ServerTenant(ctx, a, serverID)
	})
}
