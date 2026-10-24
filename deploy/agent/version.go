package agentbundle

import (
	_ "embed" // for the VERSION file
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is the edge-agent release this tree ships: the content of deploy/agent/VERSION, the one
// place the number is written. The control plane expects it and the agent reports it.
var Version = strings.TrimSpace(versionFile)
