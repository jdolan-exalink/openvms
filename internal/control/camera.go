package control

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// CameraServer implements openvmsv1.CameraServiceServer.
type CameraServer struct {
	openvmsv1.UnimplementedCameraServiceServer
	Inv      *inventory.Service
	Identity *identity.Service
}

func (s *CameraServer) ListCameras(ctx context.Context, req *openvmsv1.ListCamerasRequest) (*openvmsv1.ListCamerasResponse, error) {
	if s.Inv == nil {
		return nil, status.Error(codes.Internal, "inventory service not configured")
	}

	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return nil, err
	}

	filter := inventory.CameraFilter{}
	if req.SiteId != "" {
		id, err := uuid.Parse(strings.TrimSpace(req.SiteId))
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid site_id")
		}
		filter.SiteID = &id
	}
	if req.NodeId != "" {
		id, err := uuid.Parse(strings.TrimSpace(req.NodeId))
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid node_id")
		}
		filter.ServerID = &id
	}

	cams, err := s.Inv.ListCameras(ctx, actor, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list cameras: %v", err)
	}

	pbCams := make([]*openvmsv1.Camera, 0, len(cams))
	for _, cam := range cams {
		pbCams = append(pbCams, cameraToProto(cam))
	}

	return &openvmsv1.ListCamerasResponse{
		Cameras: pbCams,
		Pagination: &openvmsv1.Pagination{
			Page:       1,
			PageSize:   int32(len(pbCams)),
			TotalItems: int64(len(pbCams)),
			TotalPages: 1,
		},
	}, nil
}

func (s *CameraServer) GetCamera(ctx context.Context, req *openvmsv1.GetCameraRequest) (*openvmsv1.GetCameraResponse, error) {
	if s.Inv == nil {
		return nil, status.Error(codes.Internal, "inventory service not configured")
	}

	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return nil, err
	}

	camID, err := uuid.Parse(strings.TrimSpace(req.CameraId))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid camera_id")
	}

	cam, err := s.Inv.GetCamera(ctx, actor, camID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "camera not found: %v", err)
	}

	return &openvmsv1.GetCameraResponse{
		Camera: cameraToProto(cam),
	}, nil
}

func (s *CameraServer) GetCameraSnapshot(ctx context.Context, req *openvmsv1.GetCameraSnapshotRequest) (*openvmsv1.GetCameraSnapshotResponse, error) {
	if s.Inv == nil {
		return nil, status.Error(codes.Internal, "inventory service not configured")
	}

	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return nil, err
	}

	camID, err := uuid.Parse(strings.TrimSpace(req.CameraId))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid camera_id")
	}

	cam, err := s.Inv.GetCamera(ctx, actor, camID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "camera not found: %v", err)
	}

	srv, err := s.Inv.GetServer(ctx, actor, cam.ServerID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "camera server not found: %v", err)
	}

	info, err := s.Inv.ConnInfo(db.FrigateServer{
		ID:             srv.ID,
		BaseUrl:        srv.BaseUrl,
		AuthMode:       srv.AuthMode,
		Username:       srv.Username,
		PasswordSealed: srv.PasswordSealed,
		TlsSkipVerify:  srv.TlsSkipVerify,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get connection info: %v", err)
	}

	adapter, err := s.Inv.Connect(ctx, info)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "failed to connect to frigate node: %v", err)
	}

	media := adapter.Media()
	if media == nil {
		return nil, status.Error(codes.Unavailable, "media access unavailable for node")
	}

	query := url.Values{}
	if req.Height > 0 {
		query.Set("h", strconv.Itoa(int(req.Height)))
	}

	path := fmt.Sprintf("/api/%s/latest.jpg", cam.RemoteName)
	resp, err := media.Open(ctx, path, query, nil)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "failed to fetch snapshot: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, status.Errorf(codes.Unavailable, "node returned status %d for snapshot", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read snapshot body: %v", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	return &openvmsv1.GetCameraSnapshotResponse{
		ImageData:   body,
		ContentType: contentType,
		Timestamp:   time.Now().Unix(),
	}, nil
}

func cameraToProto(cam db.GetCameraRow) *openvmsv1.Camera {
	name := cam.DisplayName
	if strings.TrimSpace(name) == "" {
		name = cam.RemoteName
	}

	opStatus := openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE
	if cam.MissingSince != nil {
		opStatus = openvmsv1.OperationalStatus_OPERATIONAL_STATUS_OFFLINE
	} else if strings.ToLower(cam.Status) == "offline" {
		opStatus = openvmsv1.OperationalStatus_OPERATIONAL_STATUS_OFFLINE
	} else if strings.ToLower(cam.Status) == "degraded" {
		opStatus = openvmsv1.OperationalStatus_OPERATIONAL_STATUS_DEGRADED
	}

	var fps int32 = 15
	if cam.Fps != nil && *cam.Fps > 0 {
		fps = int32(*cam.Fps)
	}

	profiles := []*openvmsv1.StreamProfile{
		{
			Name:    "main",
			Width:   1920,
			Height:  1080,
			Fps:     fps,
			Bitrate: 4000000,
			Codec:   "h264",
		},
		{
			Name:    "sub",
			Width:   640,
			Height:  360,
			Fps:     15,
			Bitrate: 600000,
			Codec:   "h264",
		},
	}

	caps := &openvmsv1.CameraCapabilities{
		Ptz:            false,
		AudioIn:        false,
		AudioOut:       false,
		ProfilesCount:  int32(len(profiles)),
		MainCodec:      "h264",
		SubCodec:       "h264",
		Lpr:            cam.Lpr,
		Analytics:      len(cam.Zones) > 0,
		Fisheye:        false,
	}

	return &openvmsv1.Camera{
		Id:           cam.ID.String(),
		SiteId:       cam.SiteID.String(),
		NodeId:       cam.ServerID.String(),
		Name:         name,
		Status:       opStatus,
		Profiles:     profiles,
		Capabilities: caps,
		Location:     cam.Location,
		CreatedAt:    cam.CreatedAt.Unix(),
	}
}
