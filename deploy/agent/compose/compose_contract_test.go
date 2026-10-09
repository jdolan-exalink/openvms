package compose

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	// Only the gRPC control plane (9090) may be published for edge agents;
	// the HTTP API on 8080 must stay private behind the web proxy.
	if ports, ok := api["ports"].([]any); ok {
		for _, port := range ports {
			if text, isString := port.(string); isString && strings.HasSuffix(text, ":8080") {
				t.Fatalf("API HTTP must stay private; browser traffic reaches it through the web proxy, got %q", text)
			}
			if !strings.HasSuffix(fmt.Sprint(port), ":9090") {
				t.Fatalf("API may only publish the gRPC control port, got %v", port)
			}
		}
	}
	web := asMap(t, services["web"])
	webEnv := asMap(t, web["environment"])
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
	cert := exec.Command("openssl", "x509", "-in", filepath.Join(output, "tls.crt"), "-noout", "-checkip", "192.0.2.25")
	if result, err := cert.CombinedOutput(); err != nil {
		t.Fatalf("certificate does not verify its fixed IPv4 SAN: %v: %s", err, result)
	}
	trust := exec.Command("openssl", "verify", "-CAfile", filepath.Join(output, "tls.crt"), filepath.Join(output, "tls.crt"))
	if result, err := trust.CombinedOutput(); err != nil {
		t.Fatalf("self-signed certificate is not an explicit trust anchor: %v: %s", err, result)
	}
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
	token, err := os.ReadFile(filepath.Join(output, "agent.token"))
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(string(token))) != 64 {
		t.Fatalf("generated bearer token length = %d, want 64 hex characters", len(strings.TrimSpace(string(token))))
	}
	before := append([]byte(nil), token...)
	command = exec.Command("sh", script, "192.0.2.26", output)
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap unexpectedly replaced existing output: %s", result)
	}
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
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(filepath.Join(fakeBin, "openssl"), []byte(generator), 0o700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "local")
	command := exec.Command("sh", "bootstrap-local.sh", "192.0.2.25", output)
	command.Env = append(os.Environ(), "PATH="+fakeBin+":"+os.Getenv("PATH"), "TEST_OUTPUT_DIR="+output)
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap succeeded despite TLS generator failure: %s", result)
	}
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
		if !hasReadOnlyRequiredBind(t, webVolumes, target) {
			t.Errorf("web must mount TLS material read-only without creating missing host paths at %s", suffix)
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
	for _, required := range []string{"auto_https disable_redirects", "default_sni {$HTTPS_SITE_ADDRESS:10.1.1.24}", "{$SITE_ADDRESS::80} {", "{$HTTPS_SITE_ADDRESS:10.1.1.24}:443 {", "tls /etc/caddy/tls.crt /etc/caddy/tls.key", "protocols tls1.2 tls1.3", "import openvms_site_routes"} {
		if !strings.Contains(configuration, required) {
			t.Errorf("Caddy configuration must include %q", required)
		}
	}
	if strings.Contains(configuration, "redir ") {
		t.Fatal("adding HTTPS must not redirect or otherwise change existing HTTP behavior")
	}
	if strings.Count(configuration, "import openvms_site_routes") != 2 {
		t.Fatal("HTTP and HTTPS listeners must reuse identical web/API routes")
	}
}

func hasReadOnlyRequiredBind(t *testing.T, volumes []any, target string) bool {
	t.Helper()
	for _, item := range volumes {
		mount, ok := item.(map[string]any)
		if !ok || mount["target"] != target || mount["type"] != "bind" || mount["read_only"] != true {
			continue
		}
		bind, ok := mount["bind"].(map[string]any)
		return ok && bind["create_host_path"] == false
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
	command = exec.Command("sh", script, "10.1.1.25", output)
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("bootstrap unexpectedly replaced existing TLS material: %s", result)
	}
	marker, err := os.ReadFile(filepath.Join(output, "marker"))
	if err != nil || string(marker) != "preserve" {
		t.Fatalf("existing destination was modified: marker=%q err=%v", marker, err)
	}
}

func TestWebTLSBootstrapRejectsInvalidIPv4BeforeCreatingOutput(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "web-tls")
	script := filepath.Join("..", "..", "..", "deploy", "docker", "bootstrap-local-tls.sh")
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
	if err := os.WriteFile(filepath.Join(fakeBin, "mkdir"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
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
