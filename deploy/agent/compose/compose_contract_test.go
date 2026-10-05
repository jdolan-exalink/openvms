package compose

import (
	"crypto/x509"
	"encoding/pem"
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
