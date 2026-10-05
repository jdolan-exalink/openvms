package api

import (
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func TestAgentTLSConfigOperationsAreInOpenAPI(t *testing.T) {
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	path := spec.Paths.Find("/api/v1/servers/{serverId}/agent/tls")
	if path == nil || path.Get == nil || path.Put == nil || path.Delete == nil {
		t.Fatal("server-scoped TLS config must declare GET, PUT, and DELETE operations")
	}
}
