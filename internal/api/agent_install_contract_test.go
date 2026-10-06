package api

import (
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func TestAgentInstallCreateAndProgressRoutesAreInOpenAPI(t *testing.T) {
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	create := spec.Paths.Find("/api/v1/servers/{serverId}/agent/install")
	progress := spec.Paths.Find("/api/v1/servers/{serverId}/agent/install/{jobId}")
	if create == nil || create.Post == nil || progress == nil || progress.Get == nil {
		t.Fatal("server-scoped agent install and progress routes must be declared")
	}
}
