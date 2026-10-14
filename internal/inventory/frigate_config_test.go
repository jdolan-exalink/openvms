package inventory

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestAddCameraToFrigateValidation(t *testing.T) {
	s := &Service{}
	actor := authz.Actor{UserID: uuid.New()}
	serverID := uuid.New()

	t.Run("missing name and title", func(t *testing.T) {
		_, err := s.AddCameraToFrigate(context.Background(), actor, serverID, AddCameraToFrigateInput{
			MainStreamURL: "rtsp://10.0.0.1/live",
		})
		if err == nil {
			t.Fatal("expected error for empty name and title")
		}
	})

	t.Run("invalid camera name characters", func(t *testing.T) {
		_, err := s.AddCameraToFrigate(context.Background(), actor, serverID, AddCameraToFrigateInput{
			Name:          "Cam Entrance #1!",
			MainStreamURL: "rtsp://10.0.0.1/live",
		})
		if err == nil {
			t.Fatal("expected error for invalid characters in name")
		}
	})

	t.Run("missing main stream url", func(t *testing.T) {
		_, err := s.AddCameraToFrigate(context.Background(), actor, serverID, AddCameraToFrigateInput{
			Name: "cam_entrance",
		})
		if err == nil {
			t.Fatal("expected error for empty main_stream_url")
		}
	})
}
