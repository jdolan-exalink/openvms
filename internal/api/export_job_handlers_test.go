package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestExportJobsRoutesRequireAuthentication(t *testing.T) {
	h := &Handlers{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	router, err := NewRouter(h, h.Log, Options{Queries: db.New(nil)})
	if err != nil {
		t.Fatal(err)
	}

	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/export-jobs"},
		{http.MethodPost, "/api/v1/export-jobs"},
		{http.MethodGet, "/api/v1/export-jobs/" + uuid.NewString()},
		{http.MethodPost, "/api/v1/export-jobs/" + uuid.NewString() + "/cancel"},
		{http.MethodPost, "/api/v1/export-jobs/" + uuid.NewString() + "/retry"},
		{http.MethodDelete, "/api/v1/export-jobs/" + uuid.NewString()},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: expected 401, got %d", route.method, route.path, rec.Code)
		}
	}
}

func TestToExportJobMapping(t *testing.T) {
	jobID := uuid.New()
	tenantID := uuid.New()
	userID := uuid.New()
	camID := uuid.New()
	serverID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)

	job := media.ExportJob{
		ID:               jobID,
		TenantID:         tenantID,
		RequestedBy:      userID,
		Requester:        "operator",
		Name:             "Forensic Evidence 01",
		Start:            now.Add(-10 * time.Minute),
		End:              now,
		Status:           "transferring",
		Progress:         45.5,
		TotalBytes:       1024000,
		TransferredBytes: 512000,
		SpeedBps:         8500000,
		ETASeconds:       60,
		CameraCount:      1,
		Protected:        true,
		Manifest:         map[string]any{"version": "1.0"},
		CreatedAt:        now.Add(-1 * time.Minute),
		UpdatedAt:        now,
		Items: []media.ExportJobItem{
			{
				ID:               uuid.New(),
				JobID:            jobID,
				TenantID:         tenantID,
				CameraID:         camID,
				CameraName:       "Entrance",
				ServerID:         serverID,
				ServerName:       "frigate-main",
				Status:           "transferring",
				Progress:         45.5,
				TotalBytes:       1024000,
				TransferredBytes: 512000,
				SHA256Hash:       "abcd1234efgh5678",
				CreatedAt:        now.Add(-1 * time.Minute),
				UpdatedAt:        now,
			},
		},
	}

	dto := toExportJob(job)
	if dto.Id != jobID {
		t.Fatalf("expected id %v, got %v", jobID, dto.Id)
	}
	if dto.Name != "Forensic Evidence 01" {
		t.Fatalf("expected name %s, got %s", job.Name, dto.Name)
	}
	if dto.Status != gen.ExportJobStatusTransferring {
		t.Fatalf("expected status %s, got %s", gen.ExportJobStatusTransferring, dto.Status)
	}
	if !dto.Protected {
		t.Fatal("expected protected true")
	}
	if dto.Items == nil || len(*dto.Items) != 1 {
		t.Fatalf("expected 1 item, got %#v", dto.Items)
	}
	item := (*dto.Items)[0]
	if item.CameraName != "Entrance" || item.ServerName != "frigate-main" {
		t.Fatalf("unexpected item labels: camera=%s, server=%s", item.CameraName, item.ServerName)
	}
	if item.Sha256Hash == nil || *item.Sha256Hash != "abcd1234efgh5678" {
		t.Fatalf("expected sha256 hash, got %v", item.Sha256Hash)
	}
}
