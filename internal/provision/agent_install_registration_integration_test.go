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
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
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
				agentInstallRegister:        func(context.Context, authz.Actor, uuid.UUID, string, uint16, string, []byte, bool) error { return nil },
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

func TestRegisterInstalledAgentReplace(t *testing.T) {
	ctx := context.Background()
	pool, _ := pgtest.Migrated(t)
	st := &store.Store{Pool: pool}
	sealer, err := secrets.NewSealer([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	tenant, site := uuid.New(), uuid.New()
	actor := authz.Actor{System: true}
	// connectedServer mimics a ConnectAgent registration (by-address metadata included);
	// sshServer mimics a previous SSH install being refreshed without confirmation.
	connectedServer, sshServer := uuid.New(), uuid.New()
	if err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`, tenant, tenant.String(), "Install register tenant"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, $3)`, site, tenant, "Install register site")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	seed := func(serverID uuid.UUID, variant, baseURL string) {
		t.Helper()
		if err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, $4, 'http://example.invalid')`, serverID, tenant, site, "install-register-"+variant); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO server_agents (server_id, tenant_id, host, port, variant, token_sealed, version, base_url, request_headers_sealed) VALUES ($1, $2, '192.0.2.44', 7419, $3, '\x00', '0.4.0', $4, '\x00')`, serverID, tenant, variant, baseURL); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO server_agent_tls (server_id, tenant_id, secure_port, trust_mode, ca_pem) VALUES ($1, $2, 7443, 'custom', 'old-ca')`, serverID, tenant)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	seed(connectedServer, "remote", "http://192.0.2.44:7419")
	seed(sshServer, "ssh", "")

	svc := &Service{Store: st, Sealer: sealer,
		requireAgentInstallPermissions: func(context.Context, authz.Actor, uuid.UUID) error { return nil }}
	caPEM, _, err := newAgentInstallTLSCredentials("192.0.2.44")
	if err != nil {
		t.Fatal(err)
	}

	// Without confirmation an existing registration is never overwritten.
	if err := svc.registerInstalledAgent(ctx, actor, sshServer, "192.0.2.44", 7443, "fresh-token", caPEM, false); !errors.Is(err, ErrAgentAlreadyProvisioned) {
		t.Fatalf("register without replace = %v, want %v", err, ErrAgentAlreadyProvisioned)
	}

	// A confirmed reinstall replaces the credentials and clears the by-address metadata.
	if err := svc.registerInstalledAgent(ctx, actor, connectedServer, "192.0.2.44", 7443, "fresh-token", caPEM, true); err != nil {
		t.Fatal(err)
	}
	var row db.ServerAgent
	var tlsRow db.ServerAgentTl
	if err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		row, err = q.GetServerAgent(ctx, connectedServer)
		if err != nil {
			return err
		}
		tlsRow, err = q.GetServerAgentTLS(ctx, connectedServer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if row.Variant != "ssh" || row.BaseUrl != "" || len(row.RequestHeadersSealed) != 0 {
		t.Fatalf("replaced row = %+v, want ssh variant without by-address metadata", row)
	}
	opened, err := sealer.Open(row.TokenSealed, connectedServer[:])
	if err != nil {
		t.Fatal(err)
	}
	if string(opened) != "fresh-token" {
		t.Fatalf("stored token = %q, want the fresh install token", opened)
	}
	if tlsRow.SecurePort != 7443 || tlsRow.CaPem == nil || *tlsRow.CaPem == "old-ca" {
		t.Fatalf("replaced TLS row = %+v, want the fresh trust anchor", tlsRow)
	}
}
