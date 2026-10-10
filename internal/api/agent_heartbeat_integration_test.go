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

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/mtls"
	"github.com/jdolan-exalink/openvms/internal/agentauth"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentenroll"
	"github.com/jdolan-exalink/openvms/internal/agentstatus"
	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/control"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// The whole path, with nothing faked on the server side: an agent redeems a token over HTTPS
// against the real router, dials the real agent listener with the certificate it received,
// and its heartbeats show up where the API reports server status.
func TestAgentHeartbeatIsPersistedAndSurfacedByTheServerAPI(t *testing.T) {
	ctx := context.Background()
	env := demofix.Setup(t)
	agentCA := &agentca.Service{Repo: &agentca.PgRepo{Store: env.Store}, Sealer: env.Sealer}
	enrollSvc := &agentenroll.Service{Store: env.Store, Authz: env.Svc, CA: agentCA}
	h := &api.Handlers{Inv: env.Svc, Log: pgtest.Discard(), AgentEnroll: enrollSvc}
	router, err := api.NewRouter(h, pgtest.Discard(), api.Options{Queries: db.New(env.Pool)})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(router)
	defer ts.Close()

	get := func(path, bearer string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	server := env.Cameras["frigate-h01/acceso_norte"]
	serverPath := "/api/v1/servers/" + server.ServerID.String()
	readServer := func() map[string]any {
		t.Helper()
		code, body := get(serverPath, env.AdminToken)
		if code != http.StatusOK {
			t.Fatalf("GET server: %d %s", code, body)
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Before any heartbeat the agent fields are absent, not zero values.
	for _, k := range []string{"agent_last_seen_at", "agent_version", "agent_status"} {
		if _, present := readServer()[k]; present {
			t.Fatalf("%s present before the agent ever reported", k)
		}
	}

	// Enroll through the real endpoint and keep the credentials like the edge agent does.
	tok, err := enrollSvc.CreateToken(ctx, env.Admin, server.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	creds, err := mtls.Enroll(ctx, ts.Client(), ts.URL+"/api/v1/agent/enroll", tok.Value)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Identity.ServerID != server.ServerID {
		t.Fatalf("enrolled for server %s, want %s", creds.Identity.ServerID, server.ServerID)
	}
	store := mtls.Store{Dir: t.TempDir()}
	if err := store.Save(creds); err != nil {
		t.Fatal(err)
	}

	// The real agent listener: mutual TLS, revocation lookups in Postgres, heartbeats persisted.
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
		Credentials: serverCreds,
		Verifier:    &agentauth.Verifier{Certs: &agentca.PgRepo{Store: env.Store}},
		Heartbeats:  &agentstatus.Recorder{Store: env.Store},
		Renewer:     enrollSvc,
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

	worker := agent.NewWorker(agent.WorkerConfig{
		ControlGRPCAddr: lis.Addr().String(), TLSCAFile: certFile,
		HeartbeatInterval: 50 * time.Millisecond, MTLS: mtls.NewManager(store, creds),
	})
	wctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { _ = worker.Run(wctx); close(done) }()
	defer func() { stop(); <-done }()

	var got map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got = readServer(); got["agent_last_seen_at"] != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got["agent_last_seen_at"] == nil {
		t.Fatalf("the heartbeat never showed up on the server: %v", got)
	}
	seen, err := time.Parse(time.RFC3339Nano, got["agent_last_seen_at"].(string))
	if err != nil || time.Since(seen) > time.Minute {
		t.Fatalf("agent_last_seen_at = %v (%v), want a recent time", got["agent_last_seen_at"], err)
	}
	if got["agent_version"] != agent.Version || got["agent_status"] != "online" {
		t.Fatalf("agent_version/agent_status = %v/%v, want %s/online", got["agent_version"], got["agent_status"], agent.Version)
	}

	// The list endpoint carries the same fields for that server.
	code, body := get("/api/v1/servers", env.AdminToken)
	if code != http.StatusOK {
		t.Fatalf("GET servers: %d %s", code, body)
	}
	var list struct{ Items []map[string]any }
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	var listed int
	for _, s := range list.Items {
		hasAgent := s["agent_last_seen_at"] != nil
		if (s["id"] == server.ServerID.String()) != hasAgent {
			t.Errorf("server %v: agent_last_seen_at present = %v", s["id"], hasAgent)
		}
		if hasAgent {
			listed++
		}
	}
	if listed != 1 {
		t.Errorf("%d listed servers carry agent state, want exactly the enrolled one", listed)
	}

	// Another tenant cannot see this server, or therefore its agent state.
	other, err := env.Svc.CreateTenant(ctx, env.Admin, "otra-muni", "Otra Municipalidad")
	if err != nil {
		t.Fatal(err)
	}
	_, tokenB, err := bootstrap.TenantUser(ctx, env.Store, other.ID, "owner-b")
	if err != nil {
		t.Fatal(err)
	}
	if code, body := get(serverPath, tokenB); code != http.StatusNotFound || strings.Contains(string(body), "agent_") {
		t.Errorf("cross-tenant GET: %d %s, want 404 without agent state", code, body)
	}
	_, body = get("/api/v1/servers", tokenB)
	if strings.Contains(string(body), "agent_last_seen_at") || strings.Contains(string(body), server.ServerID.String()) {
		t.Errorf("another tenant's list leaks the server or its agent state: %s", body)
	}
}
