package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/realtime"
)

var eventSeq atomic.Int64

// EventServer implements openvmsv1.EventServiceServer.
type EventServer struct {
	openvmsv1.UnimplementedEventServiceServer
	Hub      *realtime.Hub
	Identity *identity.Service
	Log      *slog.Logger
}

func (s *EventServer) SubscribeEvents(stream grpc.BidiStreamingServer[openvmsv1.EventSubscription, openvmsv1.OpenVMSEvent]) error {
	ctx := stream.Context()
	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return err
	}

	if s.Hub == nil {
		return status.Error(codes.Internal, "realtime hub not configured")
	}

	sub, err := s.Hub.Subscribe(actor)
	if err != nil {
		return status.Errorf(codes.ResourceExhausted, "failed to create realtime subscription: %v", err)
	}
	defer sub.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Goroutine to receive client subscription filter updates
	errCh := make(chan error, 2)
	go func() {
		for {
			filterReq, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					// Client finished sending; keep receiving until stream cancels
					return
				}
				errCh <- err
				return
			}

			// Apply site filter
			if len(filterReq.SiteIds) > 0 {
				var siteIDs []uuid.UUID
				for _, sid := range filterReq.SiteIds {
					if id, err := uuid.Parse(strings.TrimSpace(sid)); err == nil {
						siteIDs = append(siteIDs, id)
					}
				}
				sub.SetSiteIDs(siteIDs)
			}

			// Apply event type topics
			if len(filterReq.EventTypes) > 0 {
				topics := make([]string, 0, len(filterReq.EventTypes))
				for _, et := range filterReq.EventTypes {
					switch et {
					case openvmsv1.EventType_EVENT_TYPE_CAMERA_STATUS:
						topics = append(topics, "camera")
					case openvmsv1.EventType_EVENT_TYPE_NODE_STATUS:
						topics = append(topics, "server")
					case openvmsv1.EventType_EVENT_TYPE_ALARM:
						topics = append(topics, "alarm")
					case openvmsv1.EventType_EVENT_TYPE_PERSON,
						openvmsv1.EventType_EVENT_TYPE_VEHICLE,
						openvmsv1.EventType_EVENT_TYPE_MOTION,
						openvmsv1.EventType_EVENT_TYPE_LPR:
						topics = append(topics, "object")
					}
				}
				sub.SetTopics(topics)
			}
		}
	}()

	// Main loop sending events to desktop client
	go func() {
		for {
			env, err := sub.Recv(ctx)
			if err != nil {
				errCh <- err
				return
			}

			pbEvent := mapEnvelopeToProto(env)
			if err := stream.Send(pbEvent); err != nil {
				errCh <- err
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, realtime.ErrClosed) {
			return nil
		}
		return status.Errorf(codes.Internal, "stream error: %v", err)
	}
}

func (s *EventServer) PublishEvent(ctx context.Context, req *openvmsv1.OpenVMSEvent) (*openvmsv1.PublishEventResponse, error) {
	if s.Hub == nil {
		return nil, status.Error(codes.Internal, "realtime hub not configured")
	}

	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return nil, err
	}

	seq := eventSeq.Add(1)
	req.SequenceId = seq
	if req.TimestampUnixNano == 0 {
		req.TimestampUnixNano = time.Now().UnixNano()
	}

	msgType := mapProtoEventTypeToString(req.Type)

	var siteID, cameraID, serverID *uuid.UUID
	if req.SiteId != "" {
		if id, err := uuid.Parse(req.SiteId); err == nil {
			siteID = &id
		}
	}
	if req.CameraId != "" {
		if id, err := uuid.Parse(req.CameraId); err == nil {
			cameraID = &id
		}
	}
	if req.NodeId != "" {
		if id, err := uuid.Parse(req.NodeId); err == nil {
			serverID = &id
		}
	}

	tenantID := uuid.Nil
	if actor.TenantID != nil {
		tenantID = *actor.TenantID
	}

	env := realtime.Envelope{
		V:        1,
		ID:       req.Id,
		Type:     msgType,
		TenantID: tenantID,
		SiteID:   siteID,
		CameraID: cameraID,
		ServerID: serverID,
		Data:     json.RawMessage(req.PayloadJson),
	}

	s.Hub.Publish(realtime.Message{
		Seq:      uint64(seq),
		Envelope: env,
	})

	return &openvmsv1.PublishEventResponse{
		Accepted:   true,
		SequenceId: seq,
	}, nil
}

func mapEnvelopeToProto(env realtime.Envelope) *openvmsv1.OpenVMSEvent {
	eventType := mapStringToProtoEventType(env.Type)

	siteID := ""
	if env.SiteID != nil {
		siteID = env.SiteID.String()
	}
	cameraID := ""
	if env.CameraID != nil {
		cameraID = env.CameraID.String()
	}
	nodeID := ""
	if env.ServerID != nil {
		nodeID = env.ServerID.String()
	}

	var tsNano int64
	if env.TS != nil {
		tsNano = env.TS.UnixNano()
	} else {
		tsNano = time.Now().UnixNano()
	}

	seq := eventSeq.Add(1)

	return &openvmsv1.OpenVMSEvent{
		Id:                env.ID,
		SiteId:            siteID,
		NodeId:            nodeID,
		CameraId:          cameraID,
		TimestampUnixNano: tsNano,
		Type:              eventType,
		SequenceId:        seq,
		PayloadJson:       string(env.Data),
	}
}

func mapStringToProtoEventType(s string) openvmsv1.EventType {
	switch {
	case strings.HasPrefix(s, "alarm"):
		return openvmsv1.EventType_EVENT_TYPE_ALARM
	case s == realtime.TypeCameraStatusChanged || s == "camera.status":
		return openvmsv1.EventType_EVENT_TYPE_CAMERA_STATUS
	case s == realtime.TypeServerStatus || s == realtime.TypeServerStatusChanged:
		return openvmsv1.EventType_EVENT_TYPE_NODE_STATUS
	case s == realtime.TypeEventCreated || s == realtime.TypeObjectDetected:
		return openvmsv1.EventType_EVENT_TYPE_PERSON
	default:
		return openvmsv1.EventType_EVENT_TYPE_SYSTEM
	}
}

func mapProtoEventTypeToString(t openvmsv1.EventType) string {
	switch t {
	case openvmsv1.EventType_EVENT_TYPE_ALARM:
		return realtime.TypeAlarmCreated
	case openvmsv1.EventType_EVENT_TYPE_CAMERA_STATUS:
		return realtime.TypeCameraStatusChanged
	case openvmsv1.EventType_EVENT_TYPE_NODE_STATUS:
		return realtime.TypeServerStatusChanged
	case openvmsv1.EventType_EVENT_TYPE_MOTION,
		openvmsv1.EventType_EVENT_TYPE_PERSON,
		openvmsv1.EventType_EVENT_TYPE_VEHICLE,
		openvmsv1.EventType_EVENT_TYPE_LPR:
		return realtime.TypeEventCreated
	default:
		return "system.event"
	}
}
