package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agentenroll"
	"github.com/jdolan-exalink/openvms/internal/platform/config"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

func TestConfigureAgentEnrollmentIsOffWithoutConfiguration(t *testing.T) {
	p := &provision.Service{}
	if err := configureAgentEnrollment(p, config.Config{}, &agentenroll.Service{}); err != nil {
		t.Fatal(err)
	}
	if p.AgentMTLS != nil || p.NewEnrollToken != nil {
		t.Fatal("SSH installs were changed without the enrollment settings")
	}
}

func TestConfigureAgentEnrollmentReadsTheCABundlesAtStartup(t *testing.T) {
	ca, _ := grpctlstest.WriteSelfSigned(t)
	cfg := config.Config{AgentEnroll: config.AgentEnrollConfig{GRPCAddr: "vms:9443", APIURL: "https://vms", GRPCCAFile: ca, ServerName: "vms"}}
	p := &provision.Service{}
	if err := configureAgentEnrollment(p, cfg, &agentenroll.Service{}); err != nil {
		t.Fatal(err)
	}
	if p.AgentMTLS == nil || p.NewEnrollToken == nil || p.AgentMTLS.GRPCAddr != "vms:9443" || len(p.AgentMTLS.GRPCCAPEM) == 0 || p.AgentMTLS.ServerName != "vms" {
		t.Fatalf("AgentMTLS = %+v", p.AgentMTLS)
	}

	cfg.AgentEnroll.GRPCCAFile = filepath.Join(t.TempDir(), "missing.pem")
	if err := configureAgentEnrollment(&provision.Service{}, cfg, &agentenroll.Service{}); err == nil {
		t.Fatal("a missing CA file did not stop startup")
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	_ = os.WriteFile(empty, []byte("nope"), 0o600)
	cfg.AgentEnroll.GRPCCAFile = empty
	if err := configureAgentEnrollment(&provision.Service{}, cfg, &agentenroll.Service{}); err == nil {
		t.Fatal("a CA file without certificates did not stop startup")
	}
}
