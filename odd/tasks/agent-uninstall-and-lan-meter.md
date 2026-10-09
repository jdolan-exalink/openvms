# Agent uninstall and premium LAN interface monitor

Deliver a polished, premium experience for edge-agent lifecycle management and network monitoring:
1. Filter network interfaces to show only physical LAN links (excluding loopback, Docker bridges, veth pairs, and virtual adapters).
2. Modernize the network meter in the Servers monitor into a sleek, premium interactive component that displays a real-time sparkline history of throughput on hover.
3. Implement remote agent uninstallation over SSH via API and frontend, stopping and disabling the service, removing binaries and configuration, and purging database registrations while preserving security safeguards.

## Scope and constraints

- **Security & credentials**: Transient root SSH passwords are required for uninstallation, sent only over HTTPS or allowlisted proxies, never persisted, echoed, or logged.
- **Remote boundary**: SSH operations on target hosts run systemctl service teardown, remove unit files and binaries, and reload daemon cleanly.
- **LAN-only filtering**: Exclude `lo`, `docker*`, `br-*`, `veth*`, `virbr*`, `dummy*`, `tun*`, `tap*`, `cni*`, `flannel*`, `kube*`, `vnet*`, `sit*`, `ip6tnl*`. Physical LAN links (e.g., `eth*`, `en*`, `wl*`, `bond*`) are retained.
- **Client history buffer**: Traffic history is buffered smoothly in memory on the client across refetch intervals to avoid database bloat and deliver immediate interactive sparklines.

## Work units

| ID | Title | Scope |
|---|---|---|
| `UNLAN-01` | Physical LAN interface filtering | Filter out non-LAN interfaces in `internal/agent/network.go` and add defensive client filtering in `apps/web/src/routes/Servers.tsx`. |
| `UNLAN-02` | Premium LAN meter with hover history | Implement an M3-expressive, interactive network meter with down/up activity indicators and a hover tooltip showing throughput stats and an SVG sparkline history. |
| `UNLAN-03` | Remote agent uninstallation flow | Add `POST /api/v1/servers/{serverId}/agent/uninstall` and job polling in API/provision, and add an uninstallation dialog with live progress bar and terminal logs in the UI. |

## Progress

- [x] `UNLAN-01` — Physical LAN interface filtering
- [x] `UNLAN-02` — Premium LAN meter with hover history
- [x] `UNLAN-03` — Remote agent uninstallation flow

## Verification

- Backend unit tests (`internal/agent/...`, `internal/provision/...`) pass.
- Frontend test suite (923 vitest tests across 122 files) passes.
- Live verification on target host `10.1.1.144` (`c420319a-7c77-4489-b644-3e212673dd80`):
  - Agent uninstallation: successfully stopped/disabled systemd service, purged `/usr/local/bin/openvms-agent` and `/etc/openvms`, purged database registration.
  - Agent installation: successfully provisioned, verified, and started edge agent over SSH.
  - LAN filtering: verified agent reports strictly physical LAN interfaces (`eth0`), excluding `docker0`, `br-*`, `veth*`.
  - Agent update: verified SSH update flow succeeds and health check passes.

