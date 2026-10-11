//go:build integration

package api_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/agent/mtls"
	"github.com/jdolan-exalink/openvms/internal/agentauth"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentenroll"
	"github.com/jdolan-exalink/openvms/internal/agentstatus"
	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/control"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// F4: two tenants, each with an enrolled agent on the same agent listener. Heartbeats, the
// server reads and the certificate expiry stay inside their tenant, an agent cannot speak for
// the other tenant's server, and revoking one tenant's agent leaves the other untouched.
func TestTwoTenantsAgentsStayIsolated(t *testing.T) {
	ctx := context.Background()
	env := demofix.Setup(t)
	agentCA := &agentca.Service{Repo: &agentca.PgRepo{Store: env.Store}, Sealer: env.Sealer}
	enrollSvc := &agentenroll.Service{Store: env.Store, Authz: env.Svc, CA: agentCA,
		RequireManage: func(ctx context.Context, a authz.Actor, id uuid.UUID) error {
			_, err := env.Svc.RequireServerManage(ctx, a, id)
			return err
		}}
	router, err := api.NewRouter(&api.Handlers{Inv: env.Svc, Log: pgtest.Discard(), AgentEnroll: enrollSvc}, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(router)
	defer ts.Close()

	// Tenant A is the demo tenant; tenant B is new with a server of its own. Each has one
	// user who may view and manage servers in their own tenant only.
	serverA := env.Cameras["frigate-h01/acceso_norte"].ServerID
	tenantA := env.Demo.TenantID
	tenantB, err := env.Svc.CreateTenant(ctx, env.Admin, "isolation-b", "Isolation B")
	if err != nil {
		t.Fatal(err)
	}
	serverB := seedTenantServer(t, env.Store, tenantB.ID)
	userToken := func(tenant uuid.UUID, name string) string {
		actor, token, err := bootstrap.TenantUser(ctx, env.Store, tenant, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []authz.Permission{authz.ServersView, authz.ServersManage} {
			if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
				SubjectType: "user", SubjectID: actor.UserID, Permission: p,
				Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenant,
			}); err != nil {
				t.Fatal(err)
			}
		}
		return token
	}
	tokenA, tokenB := userToken(tenantA, "iso-user-a"), userToken(tenantB.ID, "iso-user-b")

	call := func(method, path, bearer string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, method, ts.URL+path, strings.NewReader(""))
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	readServer := func(token string, id uuid.UUID) (int, map[string]any) {
		code, body := call(http.MethodGet, "/api/v1/servers/"+id.String(), token)
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return code, out
	}

	// One enrolled agent per tenant on one listener.
	enrollFor := func(server uuid.UUID) *mtls.Credentials {
		tok, err := enrollSvc.CreateToken(ctx, env.Admin, server)
		if err != nil {
			t.Fatal(err)
		}
		creds, err := mtls.Enroll(ctx, ts.Client(), ts.URL+"/api/v1/agent/enroll", tok.Value)
		if err != nil {
			t.Fatal(err)
		}
		return creds
	}
	credsA, credsB := enrollFor(serverA), enrollFor(serverB)
	if credsA.Identity.TenantID != tenantA || credsB.Identity.TenantID != tenantB.ID {
		t.Fatalf("identities = %+v / %+v", credsA.Identity, credsB.Identity)
	}
	ca, err := agentCA.LoadOrCreateCA(ctx)
	if err != nil {
		t.Fatal(err)
	}
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(ca.Cert)
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	serverCreds, err := grpctls.ServerMTLSCredentials(certFile, keyFile, clientCAs)
	if err != nil {
		t.Fatal(err)
	}
	agentSrv, err := control.NewAgentServer(control.AgentConfig{
		Credentials: serverCreds, Verifier: &agentauth.Verifier{Certs: &agentca.PgRepo{Store: env.Store}},
		Heartbeats: &agentstatus.Recorder{Store: env.Store}, Renewer: enrollSvc,
	})
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = agentSrv.GRPCServer().Serve(lis) }()
	defer agentSrv.Stop()

	dial := func(c *mtls.Credentials) openvmsv1.NodeServiceClient {
		mgr := mtls.NewManager(mtls.Store{Dir: t.TempDir()}, c)
		cc, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: certFile, GetClientCertificate: mgr.GetClientCertificate})
		if err != nil {
			t.Fatal(err)
		}
		conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(cc))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return openvmsv1.NewNodeServiceClient(conn)
	}
	beat := func(c openvmsv1.NodeServiceClient, server uuid.UUID, version string, st openvmsv1.OperationalStatus) error {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err := c.Heartbeat(cctx, &openvmsv1.NodeHeartbeatRequest{NodeId: server.String(), Version: version, Status: st})
		return err
	}
	agentA, agentB := dial(credsA), dial(credsB)
	if err := beat(agentA, serverA, "1.0.0-a", openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE); err != nil {
		t.Fatalf("agent A: %v", err)
	}
	if err := beat(agentB, serverB, "2.0.0-b", openvmsv1.OperationalStatus_OPERATIONAL_STATUS_DEGRADED); err != nil {
		t.Fatalf("agent B: %v", err)
	}

	// Each tenant reads its own server with its own agent state, and only that.
	check := func(name string, token string, id uuid.UUID, version, st string, creds *mtls.Credentials) {
		t.Helper()
		code, got := readServer(token, id)
		if code != http.StatusOK || got["agent_version"] != version || got["agent_status"] != st {
			t.Fatalf("%s reads its server: %d %v; want version %s status %s", name, code, got, version, st)
		}
		exp, err := time.Parse(time.RFC3339Nano, got["agent_cert_expires_at"].(string))
		if err != nil || !exp.Truncate(time.Second).Equal(creds.NotAfter().Truncate(time.Second)) {
			t.Fatalf("%s: agent_cert_expires_at = %v (%v), want %v", name, got["agent_cert_expires_at"], err, creds.NotAfter())
		}
	}
	check("tenant A", tokenA, serverA, "1.0.0-a", "online", credsA)
	check("tenant B", tokenB, serverB, "2.0.0-b", "degraded", credsB)
	if code, body := readServer(tokenA, serverB); code != http.StatusNotFound || body["agent_version"] != nil {
		t.Errorf("tenant A reads tenant B's server: %d %v, want 404 without agent state", code, body)
	}
	if code, body := readServer(tokenB, serverA); code != http.StatusNotFound || body["agent_version"] != nil {
		t.Errorf("tenant B reads tenant A's server: %d %v, want 404 without agent state", code, body)
	}
	for _, tc := range []struct {
		name, token  string
		own, foreign uuid.UUID
	}{{"A", tokenA, serverA, serverB}, {"B", tokenB, serverB, serverA}} {
		code, body := call(http.MethodGet, "/api/v1/servers", tc.token)
		if code != http.StatusOK || !strings.Contains(string(body), tc.own.String()) || strings.Contains(string(body), tc.foreign.String()) {
			t.Errorf("tenant %s list: %d, must contain its own server and never the other's: %s", tc.name, code, body)
		}
	}

	// An agent cannot speak for the other tenant's server, and a refused heartbeat changes nothing.
	if err := beat(agentA, serverB, "evil", openvmsv1.OperationalStatus_OPERATIONAL_STATUS_OFFLINE); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("agent A heartbeating as server B: %v, want PermissionDenied", err)
	}
	check("tenant B after the refused heartbeat", tokenB, serverB, "2.0.0-b", "degraded", credsB)

	// Revoking tenant A's agent (as tenant A) ends A's certificates only.
	if code, body := call(http.MethodPost, "/api/v1/servers/"+serverB.String()+"/agent/revoke-certificates", tokenA); code != http.StatusNotFound {
		t.Fatalf("tenant A revoking tenant B's agent: %d %s, want 404", code, body)
	}
	if code, body := call(http.MethodPost, "/api/v1/servers/"+serverA.String()+"/agent/revoke-certificates", tokenA); code != http.StatusOK || !strings.Contains(string(body), `"revoked":1`) {
		t.Fatalf("tenant A revoking its own agent: %d %s", code, body)
	}
	if err := beat(agentA, serverA, "1.0.0-a", openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked agent A: %v, want Unauthenticated", err)
	}
	if err := beat(agentB, serverB, "2.0.0-b", openvmsv1.OperationalStatus_OPERATIONAL_STATUS_DEGRADED); err != nil {
		t.Fatalf("agent B after A's revocation: %v", err)
	}
	if _, got := readServer(tokenA, serverA); got["agent_cert_expires_at"] != nil {
		t.Errorf("tenant A still reports a certificate expiry after revocation: %v", got["agent_cert_expires_at"])
	}
	check("tenant B after A's revocation", tokenB, serverB, "2.0.0-b", "degraded", credsB)
}

// seedTenantServer adds a site and a server to a tenant without a live Frigate.
func seedTenantServer(t *testing.T, st *store.Store, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	site, server := uuid.New(), uuid.New()
	err := st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'site-b')", site, tenant); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, username, password_sealed) VALUES ($1, $2, $3, 'srv-b', 'http://127.0.0.1:1', 'u', '\\x00')",
			server, tenant, site)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}
