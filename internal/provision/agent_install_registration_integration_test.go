//go:build integration

package provision

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

func TestStartAgentInstallRegisteredRowFallback(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	tenantID, siteID, serverID := uuid.New(), uuid.New(), uuid.New()
	actor := authz.Actor{UserID: uuid.New(), TenantID: &tenantID}
	if err := st.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`, tenantID, tenantID.String(), "Install test tenant"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, $3)`, siteID, tenantID, "Install test site"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, username, password_sealed) VALUES ($1, $2, $3, $4, $5, $6, $7)`, serverID, tenantID, siteID, "install-test", "https://frigate.example.test", "root", []byte("sealed")); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO server_agents (server_id, tenant_id, host, port, variant, token_sealed, version) VALUES ($1, $2, $3, $4, $5, $6, $7)`, serverID, tenantID, "192.0.2.44", 7443, "standard", []byte("existing-sealed-token"), "0.2.1")
		return err
	}); err != nil {
		t.Fatalf("seed registered server agent: %v", err)
	}

	for _, replaceExisting := range []bool{false, true} {
		t.Run(map[bool]string{false: "requires confirmation", true: "queues confirmed replacement"}[replaceExisting], func(t *testing.T) {
			tokenStarted := make(chan struct{})
			releaseToken := make(chan struct{})
			svc := &Service{
				Store:                          st,
				requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil },
				agentInstallExists:             nil,
				agentInstallBinary:             func() ([]byte, error) { return []byte("trusted test binary"), nil },
				AgentMTLS:                      &AgentMTLS{GRPCAddr: "agent.example.test:9443", APIURL: "https://agent.example.test"},
				NewEnrollToken: func(context.Context, authz.Actor, uuid.UUID) (string, error) {
					close(tokenStarted)
					<-releaseToken
					return "one-time-test-token", nil
				},
				agentInstallRun:             func(context.Context, AgentInstallRequest, func(string)) error { return nil },
				agentInstallEnrollmentCheck: func(context.Context, authz.Actor, uuid.UUID, time.Time) error { return nil },
				agentInstallHealthCheck:     func(context.Context, string, uint16, string, []byte) error { return nil },
				agentInstallRegister:        func(context.Context, authz.Actor, uuid.UUID, string, uint16, string, []byte) error { return nil },
			}
			request := validAgentInstallStartRequest()
			request.ReplaceExisting = replaceExisting
			job, err := svc.StartServerAgentInstall(ctx, actor, serverID, request)
			if !replaceExisting {
				if !errors.Is(err, ErrAgentAlreadyProvisioned) {
					t.Fatalf("StartServerAgentInstall error = %v, want %v", err, ErrAgentAlreadyProvisioned)
				}
				return
			}
			if err != nil {
				t.Fatalf("StartServerAgentInstall confirmed replacement: %v", err)
			}
			if job.ID == uuid.Nil || job.Status != "queued" || job.Stage != "validating" {
				t.Fatalf("confirmed replacement job = %#v, want nonzero queued job", job)
			}
			<-tokenStarted
			close(releaseToken)
		})
	}
}
