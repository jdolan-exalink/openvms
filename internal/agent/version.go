package agent

import agentbundle "github.com/jdolan-exalink/openvms/deploy/agent"

// Version is the edge-agent release the control plane expects; a host reporting any other value is
// outdated. It is the content of deploy/agent/VERSION, so a release changes one file.
var Version = agentbundle.Version
