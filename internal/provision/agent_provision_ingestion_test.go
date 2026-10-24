package provision

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

// Reading the ingestion state needs the right to manage the server, checked before the store is
// touched (the service here has no store, so reaching it would panic).
func TestGetProvisionLPRIngestionChecksTheRightToManageTheServerFirst(t *testing.T) {
	denied := errors.New("forbidden")
	svc := &Service{requireServerManage: func(context.Context, authz.Actor, uuid.UUID) error { return denied }}
	if _, err := svc.GetProvisionLPRIngestion(context.Background(), authz.Actor{}, uuid.New()); !errors.Is(err, denied) {
		t.Fatalf("err = %v, want the authorization error", err)
	}
}
