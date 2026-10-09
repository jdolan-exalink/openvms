package media

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestCreateExportJobInputValidation(t *testing.T) {
	s := &Service{}
	actor := authz.Actor{UserID: uuid.New()}
	now := time.Now().UTC()

	tests := []struct {
		name    string
		input   CreateExportJobInput
		wantErr string
	}{
		{
			name: "no cameras",
			input: CreateExportJobInput{
				CameraIDs: []uuid.UUID{},
				Start:     now.Add(-10 * time.Minute),
				End:       now,
			},
			wantErr: "at least one camera is required",
		},
		{
			name: "end before start",
			input: CreateExportJobInput{
				CameraIDs: []uuid.UUID{uuid.New()},
				Start:     now,
				End:       now.Add(-10 * time.Minute),
			},
			wantErr: "end must be after start",
		},
		{
			name: "exceeds max duration",
			input: CreateExportJobInput{
				CameraIDs: []uuid.UUID{uuid.New()},
				Start:     now.Add(-25 * time.Hour),
				End:       now,
			},
			wantErr: "at most 24 hours",
		},
		{
			name: "end in future",
			input: CreateExportJobInput{
				CameraIDs: []uuid.UUID{uuid.New()},
				Start:     now,
				End:       now.Add(10 * time.Minute),
			},
			wantErr: "cannot end in the future",
		},
		{
			name: "name too long",
			input: CreateExportJobInput{
				CameraIDs: []uuid.UUID{uuid.New()},
				Start:     now.Add(-10 * time.Minute),
				End:       now,
				Name:      strings.Repeat("a", 250),
			},
			wantErr: "name is too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.CreateExportJob(context.Background(), actor, tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
