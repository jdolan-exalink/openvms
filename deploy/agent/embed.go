// Package agentbundle is the Frigate and agent install bundle copied to a new host.
package agentbundle

import "embed"

// FS holds the one installer and the compose examples it selects from.
//
//go:embed install.sh chrony.conf openvms-agent.service compose config
var FS embed.FS
