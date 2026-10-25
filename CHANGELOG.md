# Changelog

All notable changes to OpenVMS are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Release rules are in
[docs/VERSIONING.md](docs/VERSIONING.md).

The version of OpenVMS lives in `VERSION` and the version of the edge agent in
`deploy/agent/VERSION`; a test keeps both in sync with this file.

## [Unreleased]

### Added

- Agent 0.4.0 (in progress): the agent version is one file, `deploy/agent/VERSION`, embedded in
  the API as the version it expects (it used to be a second string stuck at 0.2.1), and a check
  (`tools/agent_version.sh`, `make hooks`, CI job "Agent version") requires a new version for any
  agent change once the current one is released.
- Agent 0.4.0: install and upgrade from a GitHub release with one command
  (`deploy/agent/install-agent.sh`, workflow "Release agent" on tags `agent-vX.Y.Z`): checksum-verified
  binaries for amd64 and arm64, systemd unit, tokens, and the address and token to enter in OpenVMS.
- Agent 0.4.0: updates over the API check the binary, keep the previous one and roll back by themselves
  when the new one does not confirm; API tokens with scopes created from the agent console; agents
  reachable through a stored base URL (VPN or tunnel) with a guard on where OpenVMS may connect; agents
  that cannot call OpenVMS (NAT) install and are polled.
- Agent 0.4.0: connect an agent that already runs at an address OpenVMS can reach (LAN, VPN or a tunnel)
  from the server row, the API or `vmsctl agent-connect`: OpenVMS checks the address and the token first,
  seals them, and says what the token allows.
- Agent 0.4.0: `docs/agent/compatibility.md` lists what OpenVMS 0.4 does with agents 0.3 and how to upgrade them.
- `make install` takes the first administrator password from `OPENVMS_ADMIN_PASSWORD`
  (`OPENVMS_ADMIN_PASSWORD='...' make install`) and writes it to the new `.env` without
  printing it. It validates the password up front: one shorter than 8 characters aborts before
  anything is created or started.

### Changed

- Agent 0.4.2: The installer keeps the OpenVMS key in /etc/openvms/openvms.apikey and ends with the host addresses and the steps to connect it in OpenVMS
- Agent 0.4.1: The agent keeps serving its API and retries when it cannot reach OpenVMS to enroll, instead of exiting in a restart loop
- The simulated Frigates (`frigate-helvecia`, `frigate-cayasta`, with their demo passwords)
  moved from `docker-compose.yml` to the development-only `docker-compose.dev.yml`, so a
  production install no longer contains mock data; `make dev` uses the dev file.
- The Mosquitto MQTT broker is now optional (`mqtt` profile; enable it with
  `COMPOSE_PROFILES=mqtt`). OpenVMS reads Frigate over HTTP and does not need it. A
  `mosquitto` container from an earlier deploy keeps running until removed with
  `docker compose rm -sf mosquitto`.
- Password policy: 8 or more characters, with no composition rules (no character classes and
  no username check). Applies to the web, the API and `vmsctl`.
- README rewritten for 0.4.0: features by module, Compose v2 requirements, `make install`,
  `.env` reference, `make deploy`/`make rollback`, versions, agents and accelerators,
  security and troubleshooting.

### Fixed

- An invalid `OPENVMS_ADMIN_PASSWORD` no longer leaves the install without an administrator:
  the API creates the admin with a generated one-time password, warns in the log and forces a
  change at first login.

## [0.4.0] - 2026-10-19

Versioning restarts at 0.4.0. The earlier GitHub release `v0.3.0` (2026-10-15) stays as
history. This entry summarizes the work merged since that release (PRs #62 to #105).

### Added

- Face recognition: person registry with image sync to Frigate libraries, faces page
  (people, training from detections, recognitions), Frigate face configuration tab,
  watchlists, and person and watchlist conditions in alarm rules.
- Faces and recognitions are scoped by site and server like LPR, with no mandatory tenant;
  the person list shows the latest recognition and the first photo, and detection photos
  are available in persons.
- Alarms: correlation of events into one alarm per ongoing situation, auto-close after
  inactivity and for stale backlogs, per-server alarm mode, keyset pagination, priority
  inbox, contextual playback links, and a guided rule activation wizard.
- Alarm rules: templates, simulation, version history, optimistic locking, dedicated
  permissions, camera groups, site-timezone schedules and per-rule cooldown.
- Persisted camera zones with durable sync to Frigate, a manual sync retry endpoint and a
  zone editor connected to them.
- Phone live view: landscape and fullscreen single camera, pinch zoom, safe areas, snapshot
  grid, focus layout and camera swipe.
- HTTPS ingress with persistent self-signed certificates and trusted proxy peers.
- Initial setup: the first platform admin is created from `OPENVMS_ADMIN_PASSWORD` (random
  password when unset), together with a default tenant and site.
- Operations: `make install` (Docker and Compose v2 check, generated secrets, health wait),
  `make deploy` (backup, rollback tags, health check) and `make rollback`.
- Export and import of live view presentations.
- `.env.example` documents every variable `docker-compose.yml` reads, including the
  previously missing `OPENVMS_MASTER_KEY`, and `scripts/update-env-example.sh` regenerates
  it and adds missing variables to an existing `.env` without touching its values.
- Agent installation over SSH detects an agent that is already installed, reports its
  version against the one this OpenVMS ships (up to date / outdated) and points to the
  update flow, without overwriting it.
- Release tooling: `VERSION` files, this changelog, a consistency test and the OpenVMS
  version on the login page, read from `/version.json`.

### Changed

- Frigate 0.17 is the minimum supported version; 0.16 config paths were removed.
- Core services restart after a host reboot (compose restart policies).
- Frigate server capabilities are refreshed periodically.
- Mosquitto has a healthcheck; the web image publishes build metadata in `/version.json`;
  gRPC plaintext behavior is documented.
- Image versions are read from `VERSION` and `deploy/agent/VERSION` instead of
  `git describe` and hard-coded defaults.

### Fixed

- Frigate 0.18 face train attempt times are parsed correctly.
- Tracked objects are persisted per page, so a large LPR object sync backlog cannot pin
  the cursor.
- The Compose major version is read from the first `N.M` of the version line.
- Agent installation failures show the real reason (not root, unsupported OS or
  architecture, existing agent or files, missing systemd, transfer or activation) instead of
  "remote output was not retained", and the cause is always logged without secrets.
- Rollback is atomic and fail-safe; the deploy health check is bounded.
- Face fixes: tenant handling for platform admins, serialized watchlist deletion, rule
  evaluation of face conditions inside the transaction, hardened train assign and name
  redaction.
- The mobile shell stays on phones in landscape and stale swipe pointers are cleared.
- Updated `kin-openapi` to v0.151.0 for HIGH CVEs.

### Agent 0.3.0

- The edge agent answers `--version` (used by the installer to detect installed agents).
- The edge agent version lives in `deploy/agent/VERSION`; before this release the Go image
  defaulted to 0.2.1 while the Makefile and compose used 0.2.2.
