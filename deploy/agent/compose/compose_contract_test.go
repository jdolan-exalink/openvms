package compose

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestComposeHasOptInAgentWithSeparateHTTPAndVerifiedHTTPSConfiguration(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	services, ok := document["services"].(map[string]any)
	if !ok {
		t.Fatal("services mapping missing")
	}
	agent, ok := services["edge-agent"].(map[string]any)
	if !ok {
		t.Fatal("optional edge-agent service missing")
	}
	profiles, ok := agent["profiles"].([]any)
	if !ok || !contains(profiles, "onvif-agent") {
		t.Fatal("edge-agent must be opt-in through the onvif-agent profile")
	}
	if _, ok := agent["ports"]; ok {
		t.Fatal("agent ports must not be published on the host")
	}
	env := asMap(t, agent["environment"])
	for _, key := range []string{"OPENVMS_AGENT_LISTEN", "OPENVMS_AGENT_ONVIF_TLS_LISTEN", "TLS_CERT_FILE", "TLS_KEY_FILE", "ONVIF_ALLOWED_CIDRS", "ONVIF_DISCOVERY_INTERFACES", "OPENVMS_AGENT_TOKEN_FILE"} {
		if _, ok := env[key]; !ok {
			t.Errorf("agent environment must explicitly configure %s", key)
		}
	}
	volumes := asList(t, agent["volumes"])
	for _, required := range []string{":/etc/openvms/agent.token:ro", ":/etc/openvms/onvif/tls.crt:ro", ":/etc/openvms/onvif/tls.key:ro"} {
		if !hasSuffix(volumes, required) {
			t.Errorf("agent must mount %q read-only", required)
		}
	}
	networks := asMap(t, agent["networks"])
	network := asMap(t, networks["onvif-agent"])
	if _, ok := network["ipv4_address"]; !ok {
		t.Fatal("agent needs a fixed configurable IPv4 address")
	}
	api, ok := services["api"].(map[string]any)
	if !ok {
		t.Fatal("api service missing")
	}
	apiNetworks := asMap(t, api["networks"])
	if _, ok := apiNetworks["onvif-agent"]; !ok {
		t.Fatal("api must join the isolated agent bridge")
	}
}

func TestComposeDeclaresConfigurablePrivateAgentBridge(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	networks := asMap(t, document["networks"])
	agent := asMap(t, networks["onvif-agent"])
	ipam := asMap(t, agent["ipam"])
	config := asList(t, ipam["config"])
	if len(config) != 1 {
		t.Fatal("agent bridge must declare exactly one operator-configurable subnet")
	}
	if _, ok := asMap(t, config[0])["subnet"]; !ok {
		t.Fatal("agent subnet must be configurable")
	}
}

func TestAPIIsInternalOnlyAndUsesNoHostPortPublication(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	services := asMap(t, document["services"])
	api := asMap(t, services["api"])
	if _, ok := api["ports"]; ok {
		t.Fatal("API must stay private; browser traffic reaches it through the web proxy")
	}
	web := asMap(t, services["web"])
	webEnv := asMap(t, web["environment"])
	if webEnv["HTTPS_INGRESS_PROXY_CIDRS"] != "${HTTPS_INGRESS_PROXY_CIDRS:-}" {
		t.Fatalf("Caddy ingress proxy trust must default to empty/deny-all, got %#v", webEnv["HTTPS_INGRESS_PROXY_CIDRS"])
	}
	if webEnv["API_UPSTREAM"] != "${OPENVMS_AGENT_API_IPV4:-172.29.240.2}:8080" {
		t.Fatalf("web API upstream must remain on the fixed internal API address, got %#v", webEnv["API_UPSTREAM"])
	}
}

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected mapping, got %T", value)
	}
	return result
}

func asList(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("expected list, got %T", value)
	}
	return result
}

func contains(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestBootstrapCreatesIPVerifiedTLSAndPrivateTokenWithoutOverwrite(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL is required for the local certificate bootstrap integration test")
	}
	root := t.TempDir()
	output := filepath.Join(root, "local")
	script := filepath.Join("bootstrap-local.sh")
	// #nosec G204 -- fixed test script and documentation IP with t.TempDir output.
	command := exec.Command("sh", script, "192.0.2.25", output)
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, result)
	}
	dirInfo, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("output directory mode = %o, want 700", dirInfo.Mode().Perm())
	}
	for name, wantMode := range map[string]os.FileMode{"tls.key": 0o600, "agent.token": 0o600, "tls.crt": 0o644} {
		info, err := os.Stat(filepath.Join(output, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != wantMode {
			t.Errorf("%s mode = %o, want %o", name, info.Mode().Perm(), wantMode)
		}
	}
	// #nosec G204 -- fixed openssl test command reads the generated certificate in t.TempDir.
	cert := exec.Command("openssl", "x509", "-in", filepath.Join(output, "tls.crt"), "-noout", "-checkip", "192.0.2.25")
	if result, err := cert.CombinedOutput(); err != nil {
		t.Fatalf("certificate does not verify its fixed IPv4 SAN: %v: %s", err, result)
	}
	// #nosec G204 -- fixed openssl test command verifies only t.TempDir fixtures.
	trust := exec.Command("openssl", "verify", "-CAfile", filepath.Join(output, "tls.crt"), filepath.Join(output, "tls.crt"))
	if result, err := trust.CombinedOutput(); err != nil {
		t.Fatalf("self-signed certificate is not an explicit trust anchor: %v: %s", err, result)
	}
	// #nosec G304 -- test fixture path is derived from t.TempDir or a checked-in repository fixture.
	certPEM, err := os.ReadFile(filepath.Join(output, "tls.crt"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("certificate PEM block missing")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, DNSName: "192.0.2.25", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("generated cert is rejected by Go x509.Verify for the configured agent IP: %v", err)
	}
	// #nosec G304 -- test fixture path is derived from t.TempDir or a checked-in repository fixture.
	token, err := os.ReadFile(filepath.Join(output, "agent.token"))
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(string(token))) != 64 {
		t.Fatalf("generated bearer token length = %d, want 64 hex characters", len(strings.TrimSpace(string(token))))
	}
	before := append([]byte(nil), token...)
	// #nosec G204 -- fixed test script and documentation IP with t.TempDir output.
	command = exec.Command("sh", script, "192.0.2.26", output)
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap unexpectedly replaced existing output: %s", result)
	}
	// #nosec G304 -- test fixture path is derived from t.TempDir or a checked-in repository fixture.
	after, err := os.ReadFile(filepath.Join(output, "agent.token"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("existing token changed after refused overwrite")
	}
}

func TestBootstrapRejectsInvalidIPv4BeforeCreatingFiles(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "local")
	// #nosec G204 -- fixed test script with intentionally invalid documentation IP.
	command := exec.Command("sh", "bootstrap-local.sh", "192.0.2.999", output)
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap accepted invalid IPv4: %s", result)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid IPv4 created output directory: %v", err)
	}
}

func hasSuffix(values []any, suffix string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.HasSuffix(text, suffix) {
			return true
		}
	}
	return false
}

func TestBootstrapDoesNotReplaceDestinationCreatedImmediatelyBeforePublish(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL is required for the local certificate bootstrap integration test")
	}
	mkdirPath, err := exec.LookPath("mkdir")
	if err != nil {
		t.Skip("mkdir is required for the local certificate bootstrap integration test")
	}
	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.Mkdir(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "local")
	wrapper := "#!/bin/sh\nfor candidate do\n  if [ \"$candidate\" = \"" + output + "\" ]; then\n    \"" + mkdirPath + "\" -m 700 -- \"" + output + "\"\n    exec \"" + mkdirPath + "\" \"$@\"\n  fi\ndone\nexec \"" + mkdirPath + "\" \"$@\"\n"
	wrapperPath := filepath.Join(fakeBin, "mkdir")
	// #nosec G306 -- test fixture deliberately uses public or executable permissions.
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- fixed repository test script and t.TempDir fixture arguments.
	command := exec.Command("sh", "bootstrap-local.sh", "192.0.2.25", output)
	command.Env = append(os.Environ(), "PATH="+fakeBin+":"+os.Getenv("PATH"))
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap replaced a directory created at the publish boundary: %s", result)
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatalf("race-created destination disappeared: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("bootstrap wrote into or replaced race-created destination: %v", entries)
	}
}

func TestBootstrapCleansOnlyItsArtifactsAfterPartialFailure(t *testing.T) {
	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.Mkdir(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	generator := "#!/bin/sh\nprintf external > \"$TEST_OUTPUT_DIR/external-marker\"\nexit 1\n"
	// #nosec G306 -- test fixture deliberately uses public or executable permissions.
	if err := os.WriteFile(filepath.Join(fakeBin, "openssl"), []byte(generator), 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "local")
	// #nosec G204 -- fixed repository test script and t.TempDir fixture arguments.
	command := exec.Command("sh", "bootstrap-local.sh", "192.0.2.25", output)
	command.Env = append(os.Environ(), "PATH="+fakeBin+":"+os.Getenv("PATH"), "TEST_OUTPUT_DIR="+output)
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap succeeded despite TLS generator failure: %s", result)
	}
	// #nosec G304 -- test fixture path is derived from t.TempDir or a checked-in repository fixture.
	marker, err := os.ReadFile(filepath.Join(output, "external-marker"))
	if err != nil || string(marker) != "external" {
		t.Fatalf("cleanup removed or changed unexpected concurrent content: %q, %v", marker, err)
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "external-marker" {
		t.Fatalf("cleanup left unexpected bootstrap artifacts: %v", entries)
	}
}

func TestWebComposeAddsPinnedHTTPSIngressWithoutBreakingHTTPOrProxyTrust(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	services := asMap(t, document["services"])
	web := asMap(t, services["web"])
	ports := asList(t, web["ports"])
	if !contains(ports, "${WEB_PORT:-8000}:80") || !contains(ports, "${WEB_HTTPS_PORT:-8443}:443") {
		t.Fatalf("web must preserve HTTP :8000 and add HTTPS :8443, got %#v", ports)
	}
	webEnv := asMap(t, web["environment"])
	if webEnv["API_UPSTREAM"] != "${OPENVMS_AGENT_API_IPV4:-172.29.240.2}:8080" {
		t.Fatalf("Caddy API upstream must use the API's fixed private IPv4, got %#v", webEnv["API_UPSTREAM"])
	}
	if webEnv["OPENVMS_WEB_HTTPS_PORT"] != "${WEB_HTTPS_PORT:-8443}" {
		t.Fatalf("Caddy must receive the effective published HTTPS port for the same-origin redirect hint, got %#v", webEnv["OPENVMS_WEB_HTTPS_PORT"])
	}
	webVolumes := asList(t, web["volumes"])
	for _, suffix := range []string{":/etc/caddy/tls.crt:ro", ":/etc/caddy/tls.key:ro"} {
		target := strings.TrimSuffix(strings.TrimPrefix(suffix, ":"), ":ro")
		if !hasReadOnlyOptionalBind(t, webVolumes, target) {
			t.Errorf("web must preserve optional read-only external TLS overrides at %s", suffix)
		}
	}
	webNetworks := asMap(t, web["networks"])
	proxyNetwork := asMap(t, webNetworks["onvif-agent"])
	if proxyNetwork["ipv4_address"] != "${OPENVMS_AGENT_WEB_IPV4:-172.29.240.4}" {
		t.Fatalf("web must have its stable proxy peer address, got %#v", proxyNetwork["ipv4_address"])
	}
	api := asMap(t, services["api"])
	apiEnv := asMap(t, api["environment"])
	if apiEnv["CREDENTIAL_TRUSTED_PROXY_CIDRS"] != "${CREDENTIAL_TRUSTED_PROXY_CIDRS:-${OPENVMS_AGENT_WEB_IPV4:-172.29.240.4}/32}" {
		t.Fatalf("API credential route must trust only the fixed web peer by default, got %#v", apiEnv["CREDENTIAL_TRUSTED_PROXY_CIDRS"])
	}
	apiNetworks := asMap(t, api["networks"])
	apiProxyNetwork := asMap(t, apiNetworks["onvif-agent"])
	if apiProxyNetwork["ipv4_address"] != "${OPENVMS_AGENT_API_IPV4:-172.29.240.2}" {
		t.Fatalf("API private peer address changed: %#v", apiProxyNetwork["ipv4_address"])
	}
	if _, ok := webNetworks["default"]; !ok {
		t.Fatal("web must retain default network access")
	}
	if _, ok := apiNetworks["default"]; !ok {
		t.Fatal("API must retain default network access")
	}

	caddyfile, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "docker", "Caddyfile"))
	if err != nil {
		t.Fatal(err)
	}
	configuration := string(caddyfile)
	for _, required := range []string{"/.well-known/openvms-https-port", "{$OPENVMS_WEB_HTTPS_PORT:8443}", "Cache-Control no-store", "text/plain"} {
		if !strings.Contains(configuration, required) {
			t.Errorf("Caddy must expose a same-origin no-store HTTPS port hint, missing %q", required)
		}
	}
	portHintPosition := strings.Index(configuration, "@https_port_hint path /.well-known/openvms-https-port")
	spaFallbackPosition := strings.Index(configuration, "\n\thandle {\n")
	if portHintPosition < 0 || spaFallbackPosition < 0 || portHintPosition > spaFallbackPosition {
		t.Fatal("Caddy HTTPS port hint must be handled before the SPA catch-all")
	}
	for _, required := range []string{"auto_https disable_redirects", "default_sni {$OPENVMS_HTTPS_DEFAULT_SNI:localhost}", "trusted_proxies static {$HTTPS_INGRESS_PROXY_CIDRS}", "expression `{http.request.header.X-Forwarded-Proto}.contains(\",\")`", "{$SITE_ADDRESS::80} {", "{$HTTPS_SITE_ADDRESS::443} {", "CADDY_TLS_CERT_FILE:/var/lib/openvms/tls/current/tls.crt", "protocols tls1.2 tls1.3", "import openvms_site_routes"} {
		if !strings.Contains(configuration, required) {
			t.Errorf("Caddy configuration must include %q", required)
		}
	}
	chainGuardPosition := strings.Index(configuration, "@api_forwarded_proto_chain")
	apiHandlerPosition := strings.Index(configuration, "@api path /api/*")
	if chainGuardPosition < 0 || apiHandlerPosition < 0 || chainGuardPosition > apiHandlerPosition {
		t.Fatal("Caddy must reject comma-joined forwarded-proto values before proxying API requests")
	}
	if strings.Contains(configuration, "redir ") {
		t.Fatal("adding HTTPS must not redirect or otherwise change existing HTTP behavior")
	}
	if strings.Count(configuration, "import openvms_site_routes") != 2 {
		t.Fatal("HTTP and HTTPS listeners must reuse identical web/API routes")
	}
}

func hasReadOnlyOptionalBind(t *testing.T, volumes []any, target string) bool {
	t.Helper()
	for _, item := range volumes {
		mount, ok := item.(map[string]any)
		if !ok || mount["target"] != target || mount["type"] != "bind" || mount["read_only"] != true {
			continue
		}
		bind, ok := mount["bind"].(map[string]any)
		return ok && bind["create_host_path"] == true
	}
	return false
}

func TestWebTLSBootstrapCreatesVerifiedIPCertificateAndRefusesOverwrite(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL is required for the local certificate bootstrap integration test")
	}
	root := t.TempDir()
	output := filepath.Join(root, "web-tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "bootstrap-local-tls.sh")
	// #nosec G204 -- fixed repository test script and t.TempDir fixture arguments.
	command := exec.Command("sh", script, "10.1.1.24", output)
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("web TLS bootstrap failed: %v: %s", err, result)
	}
	if info, err := os.Stat(output); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("TLS output directory must be private: info=%v err=%v", info, err)
	}
	for name, mode := range map[string]os.FileMode{"tls.crt": 0o600, "tls.key": 0o600} {
		info, err := os.Stat(filepath.Join(output, name))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s must have mode 0600: info=%v err=%v", name, info, err)
		}
	}
	// #nosec G304 -- test fixture path is derived from t.TempDir or a checked-in repository fixture.
	certificatePEM, err := os.ReadFile(filepath.Join(output, "tls.crt"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		t.Fatal("certificate PEM missing")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, DNSName: "10.1.1.24", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("certificate does not verify for the configured IPv4 SAN: %v", err)
	}
	if err := os.WriteFile(filepath.Join(output, "marker"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- fixed repository test script and t.TempDir fixture arguments.
	command = exec.Command("sh", script, "10.1.1.25", output)
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap unexpectedly replaced existing TLS material: %s", result)
	}
	// #nosec G304 -- test fixture path is derived from t.TempDir or a checked-in repository fixture.
	marker, err := os.ReadFile(filepath.Join(output, "marker"))
	if err != nil || string(marker) != "preserve" {
		t.Fatalf("existing destination was modified: marker=%q err=%v", marker, err)
	}
}

func TestWebTLSBootstrapRejectsInvalidIPv4BeforeCreatingOutput(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "web-tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "bootstrap-local-tls.sh")
	// #nosec G204 -- fixed repository test script and t.TempDir fixture arguments.
	command := exec.Command("sh", script, "10.1.1.999", output)
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap accepted invalid IPv4: %s", result)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid IPv4 created output directory: %v", err)
	}
}

func TestWebTLSBootstrapDoesNotReplaceDestinationAtAtomicClaimBoundary(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL is required for the local certificate bootstrap integration test")
	}
	mkdirPath, err := exec.LookPath("mkdir")
	if err != nil {
		t.Skip("mkdir is required for the local certificate bootstrap integration test")
	}
	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.Mkdir(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "web-tls")
	wrapper := "#!/bin/sh\nfor candidate do\n  if [ \"$candidate\" = \"" + output + "\" ]; then\n    \"" + mkdirPath + "\" -m 700 -- \"" + output + "\"\n    exec \"" + mkdirPath + "\" \"$@\"\n  fi\ndone\nexec \"" + mkdirPath + "\" \"$@\"\n"
	// #nosec G306 -- test fixture deliberately uses public or executable permissions.
	if err := os.WriteFile(filepath.Join(fakeBin, "mkdir"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- fixed repository bootstrap script with t.TempDir output.
	command := exec.Command("sh", filepath.Join("..", "..", "..", "deploy", "docker", "bootstrap-local-tls.sh"), "10.1.1.24", output)
	command.Env = append(os.Environ(), "PATH="+fakeBin+":"+os.Getenv("PATH"))
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap replaced an output directory created immediately before its claim: %s", result)
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatalf("concurrent destination disappeared: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("bootstrap wrote into or replaced the concurrent destination: %v", entries)
	}
}

func TestComposeWebTLSUsesPersistentGeneratedCertificateVolumeAndConfiguredSANs(t *testing.T) {
	compose, err := os.ReadFile(filepath.Join("..", "..", "..", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(compose)
	for _, required := range []string{"OPENVMS_HTTPS_SANS", "openvms-web-tls:/var/lib/openvms/tls", "OPENVMS_WEB_TLS_CERT_HOST_FILE", "OPENVMS_WEB_TLS_KEY_HOST_FILE"} {
		if !strings.Contains(text, required) {
			t.Errorf("compose must preserve automatic TLS/SAN and external certificate support, missing %q", required)
		}
	}
	if strings.Contains(text, "create_host_path: false") && strings.Contains(text, "OPENVMS_WEB_TLS_CERT_HOST_FILE") {
		t.Fatal("fresh deployment must not fail because optional external certificate files are absent")
	}
}

func TestAutomaticWebTLSBootstrapGeneratesAndReusesSANCertificate(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL is required for TLS generation integration test")
	}
	root := t.TempDir()
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	run := func(sans string) {
		t.Helper()
		cmd := exec.Command("sh", script)
		cmd.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS="+sans, "OPENVMS_WEB_TLS_DIR="+root)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("prepare TLS failed: %v: %s", err, out)
		}
	}
	run("localhost,127.0.0.1,::1,openvms.test")
	certPath, keyPath := filepath.Join(root, "current", "tls.crt"), filepath.Join(root, "current", "tls.key")
	for name, mode := range map[string]os.FileMode{"tls.crt": 0o644, "tls.key": 0o600} {
		info, err := os.Stat(filepath.Join(root, "current", name))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s permissions: %v %v", name, info, err)
		}
	}
	first, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(first)
	if block == nil {
		t.Fatal("generated certificate PEM missing")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.IsCA || !parsed.BasicConstraintsValid {
		t.Fatalf("generated server certificate must be a non-CA leaf: IsCA=%v valid=%v", parsed.IsCA, parsed.BasicConstraintsValid)
	}
	if len(parsed.ExtKeyUsage) != 1 || parsed.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Fatalf("generated certificate must be serverAuth only: %#v", parsed.ExtKeyUsage)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	if _, err := parsed.Verify(x509.VerifyOptions{Roots: roots, DNSName: "openvms.test", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("generated self-signed leaf must be usable when explicitly trusted by the client: %v", err)
	}
	run("localhost,127.0.0.1,::1,openvms.test")
	second, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("matching persisted certificate must be reused")
	}
	for _, name := range []string{"localhost", "openvms.test"} {
		cmd := exec.Command("openssl", "x509", "-in", certPath, "-noout", "-checkhost", name)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("certificate missing SAN %s: %v %s", name, err, out)
		}
	}
	for _, ip := range []string{"127.0.0.1", "::1"} {
		cmd := exec.Command("openssl", "x509", "-in", certPath, "-noout", "-checkip", ip)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("certificate missing IP SAN %s: %v %s", ip, err, out)
		}
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatal(err)
	}
	run("localhost,127.0.0.1,::1,new.example")
	third, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(second, third) {
		t.Fatal("changed SAN set must rotate certificate")
	}
}

func TestAutomaticWebTLSBootstrapRejectsMalformedSANWithoutReplacingCertificate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	cmd := exec.Command("sh", script)
	cmd.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=localhost,not a host", "OPENVMS_WEB_TLS_DIR="+root)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("invalid SAN accepted: %s", out)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid SAN created TLS directory: %v", err)
	}
}

func TestAutomaticWebTLSBootstrapRejectsMalformedIPv6BeforeCreatingOutput(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	command := exec.Command("sh", script)
	command.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=2001:::db8", "OPENVMS_WEB_TLS_DIR="+root)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("invalid IPv6 SAN accepted: %s", output)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid IPv6 SAN created TLS directory: %v", err)
	}
}

func TestAutomaticWebTLSBootstrapRejectsInvalidIPv4BeforeCreatingOutput(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	command := exec.Command("sh", script)
	command.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=localhost,192.0.2.999", "OPENVMS_WEB_TLS_DIR="+root)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("invalid IPv4 SAN accepted: %s", output)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid IPv4 SAN created TLS directory: %v", err)
	}
}

func TestAutomaticWebTLSBootstrapRejectsOversizedDNSLabelBeforeCreatingOutput(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tls")
	longLabel := strings.Repeat("a", 64)
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	command := exec.Command("sh", script)
	command.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=localhost,"+longLabel+".example", "OPENVMS_WEB_TLS_DIR="+root)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("oversized DNS label accepted: %s", output)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid DNS SAN created TLS directory: %v", err)
	}
}

func TestAutomaticWebTLSBootstrapRequiresDefaultSNIInSANSet(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	command := exec.Command("sh", script)
	command.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=192.0.2.25,appliance.example", "OPENVMS_HTTPS_DEFAULT_SNI=localhost", "OPENVMS_WEB_TLS_DIR="+root)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("default SNI absent from SAN set was accepted: %s", output)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid default SNI created TLS directory: %v", err)
	}
}

func TestAutomaticWebTLSBootstrapValidatesExternalCertificatePair(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL is required for TLS generation integration test")
	}
	root := t.TempDir()
	cert, key := filepath.Join(root, "custom.crt"), filepath.Join(root, "custom.key")
	command := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "2", "-subj", "/CN=custom.test", "-addext", "subjectAltName=DNS:custom.test", "-keyout", key, "-out", cert)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create fixture certificate: %v: %s", err, output)
	}
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	command = exec.Command("sh", script)
	command.Env = append(os.Environ(), "OPENVMS_WEB_TLS_CERT_FILE="+cert, "OPENVMS_WEB_TLS_KEY_FILE="+key, "OPENVMS_WEB_TLS_DIR="+filepath.Join(root, "generated"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("valid external pair rejected: %v: %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, "generated")); !os.IsNotExist(err) {
		t.Fatalf("external mode unexpectedly generated or modified automatic TLS: %v", err)
	}
	if err := os.WriteFile(key, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	command = exec.Command("sh", script)
	command.Env = append(os.Environ(), "OPENVMS_WEB_TLS_CERT_FILE="+cert, "OPENVMS_WEB_TLS_KEY_FILE="+key, "OPENVMS_WEB_TLS_DIR="+filepath.Join(root, "generated"))
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("invalid custom pair accepted: %s", output)
	}
}

func TestAutomaticWebTLSBootstrapReleasesGenerationLockBeforeCaddyExec(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	command := exec.Command("sh", "-c", `. "$1"; test ! -d "$OPENVMS_WEB_TLS_DIR/.generation-lock"`, "test", script)
	command.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=localhost", "OPENVMS_WEB_TLS_DIR="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("entrypoint must release startup lock before exec: %v: %s", err, output)
	}
}

func TestAutomaticWebTLSBootstrapFailsActionablyOnAbandonedGenerationLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tls")
	if err := os.MkdirAll(filepath.Join(root, ".generation-lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "sh", script)
	command.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=localhost", "OPENVMS_WEB_TLS_DIR="+root, "OPENVMS_TLS_LOCK_WAIT_SECONDS=1")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("TLS bootstrap hung on a pre-existing lock instead of failing within its bound: %v: %s", ctx.Err(), output)
	}
	if err == nil {
		t.Fatalf("TLS bootstrap unexpectedly proceeded through pre-existing lock: %s", output)
	}
	if !strings.Contains(string(output), "generation lock") || !strings.Contains(string(output), "stop the web container") {
		t.Fatalf("lock failure must explain safe recovery, got: %s", output)
	}
	if _, err := os.Stat(filepath.Join(root, ".generation-lock")); err != nil {
		t.Fatalf("bootstrap must not remove or steal a pre-existing lock: %v", err)
	}
}

func TestAutomaticWebTLSBootstrapRecoversFromAbandonedGenerationDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tls")
	pidFile := filepath.Join(t.TempDir(), "startup-pid")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	command := exec.Command("sh", "-c", `printf '%s' "$$" > "$PID_MARKER"; mkdir -m 700 "$OPENVMS_WEB_TLS_DIR/.generation-$$"; exec sh "$1"`, "test", script)
	command.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=localhost", "OPENVMS_WEB_TLS_DIR="+root, "PID_MARKER="+pidFile)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("TLS bootstrap must recover from its own PID's abandoned staging directory: %v: %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, "current", "tls.crt")); err != nil {
		t.Fatalf("TLS bootstrap did not install a certificate after staging recovery: %v", err)
	}
	pid, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".generation-"+string(pid))); !os.IsNotExist(err) {
		t.Fatalf("abandoned same-PID staging directory was not removed: %v", err)
	}
}

func TestAutomaticWebTLSBootstrapRotatesNearExpiryCertificate(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL is required for TLS generation integration test")
	}
	root := filepath.Join(t.TempDir(), "tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	run := func() {
		t.Helper()
		command := exec.Command("sh", script)
		command.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=localhost", "OPENVMS_WEB_TLS_DIR="+root)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("prepare TLS: %v: %s", err, output)
		}
	}
	run()
	certPath, keyPath := filepath.Join(root, "current", "tls.crt"), filepath.Join(root, "current", "tls.key")
	command := exec.Command("openssl", "req", "-x509", "-new", "-key", keyPath, "-sha256", "-days", "1", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost", "-addext", "basicConstraints=critical,CA:FALSE", "-addext", "extendedKeyUsage=serverAuth", "-addext", "keyUsage=digitalSignature,keyEncipherment", "-out", certPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create near-expiry fixture: %v: %s", err, output)
	}
	nearExpiry, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	run()
	rotated, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(nearExpiry, rotated) {
		t.Fatal("near-expiry certificate was not rotated")
	}
	if output, err := exec.Command("openssl", "x509", "-in", certPath, "-noout", "-checkend", "2592000").CombinedOutput(); err != nil {
		t.Fatalf("replacement certificate must exceed 30-day threshold: %v %s", err, output)
	}
}

func TestAutomaticWebTLSBootstrapSerializesConcurrentStartup(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL is required for TLS generation integration test")
	}
	root := filepath.Join(t.TempDir(), "tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "prepare-web-tls.sh")
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			command := exec.Command("sh", script)
			command.Env = append(os.Environ(), "OPENVMS_HTTPS_SANS=localhost,127.0.0.1", "OPENVMS_WEB_TLS_DIR="+root)
			if output, err := command.CombinedOutput(); err != nil {
				errCh <- fmt.Errorf("concurrent TLS prepare: %w: %s", err, output)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".generation-lock")); !os.IsNotExist(err) {
		t.Fatalf("generation lock remains after concurrent startup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "current", "tls.crt")); err != nil {
		t.Fatalf("concurrent startup left no current certificate: %v", err)
	}
}
