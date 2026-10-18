package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

func (h *Handlers) ListFaceConfig(ctx context.Context, r gen.ListFaceConfigRequestObject) (gen.ListFaceConfigResponseObject, error) {
	a, tenant, err := h.faceCaller(ctx, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	list, err := h.Inv.ListFaceConfig(ctx, a, tenant)
	if err != nil {
		return nil, err
	}
	out := gen.ListFaceConfig200JSONResponse{Items: make([]gen.FaceServerConfig, 0, len(list))}
	for _, it := range list {
		item := gen.FaceServerConfig{
			ServerId: it.ServerID, ServerName: it.ServerName, SiteName: it.SiteName, FrigateVersion: it.FrigateVersion,
			CanEdit: it.CanEdit, Cameras: make([]gen.FaceCameraConfig, 0, len(it.Cameras)),
		}
		if it.Error != "" {
			msg := it.Error
			item.Error = &msg
		}
		if st := it.Settings; st != nil {
			size := gen.FaceServerConfigModelSize(st.ModelSize)
			item.Enabled, item.ModelSize = &st.Enabled, &size
			item.DetectionThreshold, item.RecognitionThreshold, item.UnknownScore = &st.DetectionThreshold, &st.RecognitionThreshold, &st.UnknownScore
			item.MinFaces, item.MinArea, item.SaveAttempts = &st.MinFaces, &st.MinArea, &st.SaveAttempts
			item.BlurConfidenceFilter = &st.BlurConfidenceFilter
		}
		for _, c := range it.Cameras {
			item.Cameras = append(item.Cameras, gen.FaceCameraConfig{CameraId: c.CameraID, Name: c.Name, Enabled: c.Enabled, EnabledOverride: c.EnabledOverride, MinArea: c.MinArea})
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func (h *Handlers) PatchFaceConfig(ctx context.Context, r gen.PatchFaceConfigRequestObject) (gen.PatchFaceConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	// The tenant is the server's own: the service re-checks faces.manage and servers.config there.
	srv, err := h.Inv.GetServer(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	in := inventory.FaceConfigPatch{}
	if b := r.Body; b != nil {
		in.Enabled, in.DetectionThreshold, in.RecognitionThreshold, in.UnknownScore = b.Enabled, b.DetectionThreshold, b.RecognitionThreshold, b.UnknownScore
		in.MinFaces, in.MinArea, in.SaveAttempts, in.BlurConfidenceFilter = b.MinFaces, b.MinArea, b.SaveAttempts, b.BlurConfidenceFilter
		if b.ModelSize != nil {
			size := string(*b.ModelSize)
			in.ModelSize = &size
		}
		if b.Cameras != nil {
			for _, c := range *b.Cameras {
				in.Cameras = append(in.Cameras, inventory.FaceCameraPatch{CameraID: c.CameraId, Enabled: c.Enabled, MinArea: c.MinArea})
			}
		}
	}
	res, err := h.Inv.PatchFaceConfig(ctx, a, srv.TenantID, r.ServerId, in)
	if err != nil {
		return nil, err
	}
	return gen.PatchFaceConfig200JSONResponse{RevisionIds: res.RevisionIDs, RestartRequired: res.RestartRequired}, nil
}
