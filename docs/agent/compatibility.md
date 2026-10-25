# Agent 0.3 and 0.4 side by side

OpenVMS 0.4 talks to agents 0.3 and 0.4. Nothing breaks when an agent is not updated yet; the features
that need the new agent say so ("update the agent") instead of failing in silence.

Facts below come from the 0.3.0 agent source (`137243f`) and from the 0.4.0 central code.

| Capability | Agent 0.3 | Agent 0.4 |
|---|---|---|
| Health, metrics, ONVIF discovery/probe/PTZ/imaging, camera configure | Yes | Yes |
| Authentication | One shared bearer secret | Same secret, plus scoped tokens `ovmsa_...` (read, events, control, update) |
| Provision-ISR camera registry (`/v1/provision-lpr/...`) | No: central gets 404 and reports `update the agent` | Yes |
| Provision-ISR readings queue and pull (`/events/after`, `/events/ack`) | No: same 404, same message | Yes (durable queue, central pulls every 15 s) |
| Update over the API (`POST /v1/update`) | Accepts an ELF binary and restarts. Ignores the checksum and version headers, keeps no previous binary, has no rollback | Verifies architecture, checksum and `--version`, keeps `.prev`, rolls back by itself if the new one does not confirm |
| Reachable by address (VPN, tunnel, `base_url`) | Yes, it is just an HTTP(S) API; central connects to it the same way | Yes |
| Heartbeat to central | Needs the agent to reach OpenVMS | Not required: central polls `/v1/metrics` |
| Install from GitHub with `curl` | No | Yes (`install-agent.sh`) |

## What OpenVMS does with a 0.3 agent

- **Status and operations** work as before, through host and port or through a stored base URL.
- **Readings and registry**: the Provision-ISR card shows that the agent must be updated. No readings
  are lost on the OpenVMS side; a 0.3 agent has no queue, so nothing is held for it.
- **Update**: the bundled 0.4 binary is pushed with the existing `POST /v1/update`. The 0.3 agent only
  checks that it is an ELF file, so a wrong-architecture binary would be accepted. Update it from the
  console after checking `uname -m` on the host, or use the installer below.
- **No automatic rollback for this one step.** The safety net only exists in the agent doing the
  update, and the 0.3 agent has none (see `update.md`, Limits). If the host is not reachable after it,
  reinstall with `install-agent.sh`.

## Upgrading a 0.3 agent

Pick one:

1. **From the host** (preferred, it verifies the checksum and keeps the previous binary):
   ```text
   curl -fsSL https://github.com/jdolan-exalink/openvms/releases/download/agent-v0.4.0/install-agent.sh | sudo bash
   ```
   See `install.md`. The existing configuration and secret are kept, so OpenVMS does not need to be
   told again.
2. **From OpenVMS** (SSH-installed agents): the server row's update action. Use it only on agents
   whose architecture you know.

After the update, the card shows the version `0.4.0` and the Provision-ISR pull starts on its own.

## Not verified

This table was built from code and tests. No 0.3 agent was updated end to end for this document.
