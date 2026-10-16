# HTTPS and agent SSH install strategy

Status: ACTIVE — scoped HTTPS trusted-ingress fix only. The broader LAN certificate/install strategy remains
unsettled and is intentionally deferred to a later product decision.

## Tracking
- Branch: `feat/https-agent-ingress`, based on `feat/alarms-phase2-rules` at `8809c418746720c097e6f177fe20092b55133ab6`.
- Route: delegated direct; strict TDD enabled by user/session configuration.
- Runners: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/api ./internal/platform/config ./deploy/agent/compose`; `GOCACHE=/tmp/openvms-go-build-cache go vet ./internal/api ./internal/platform/config ./deploy/agent/compose`; `docker compose config --quiet`; isolated Caddy runtime harness if the pinned image is locally available; `git diff --check`.
- Authored forecast: approximately 200 lines, excluding generated files; advisory 400-line work-unit heuristic.
- Delivery: `ask-on-risk` (default); if slicing is needed, use previously user-selected `stacked-to-main`. No PR creation authorized.
- Constraints: no remote operations, deployment/restart, SSH/tunnel changes, actual `.env` edits, broad private-range trust, mTLS changes, or LAN certificate policy changes.

## Tasks
- [x] HTTPS-01 — Configure explicit operator-controlled Caddy ingress proxy CIDRs (empty/default deny); preserve HTTPS forwarding only for a trusted immediate peer and enforce a single exact `https` protocol value. Add regression tests for trusted, untrusted/spoofed, duplicate/multi-hop, and ordinary HTTP inputs. Route: delegated direct; strict TDD.
- [ ] HTTPS-02 — Decide LAN endpoint/certificate strategy (pending product choice; no implementation in this task).
- [x] HTTPS-03 — Close with focused/broader verification, isolated runtime evidence, work-unit commit, and tracker/mirror sync.

## Alarms closeout context
- P2-05 native review was approved and acknowledged; authority burned for `review-41b36ff5391a45cd`.
- R3-001 stale preview is a nonblocking separate follow-up; older accumulated-range review was unavailable due to budget.
- User authorized proceeding with this HTTPS fix; Alarms phases 3–6 remain incomplete and are not marked done here.

## Progress and evidence
- RED: the focused compose contract failed before Caddy changes because the explicit ingress trust and comma-value guard were missing. An initial isolated Caddy v2.11.7 harness also exposed that `reverse_proxy` normalized duplicate `X-Forwarded-Proto` fields to one value, which would otherwise let the API accept a duplicate. The guard now rejects comma-joined values before proxying.
- GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/api ./internal/platform/config ./deploy/agent/compose` passed; `GOCACHE=/tmp/openvms-go-build-cache go vet ./internal/api ./internal/platform/config ./deploy/agent/compose` passed; `docker compose config --quiet` and `git diff --check` passed. Caddy v2.11.7 `adapt --validate` passed with a single and multi-CIDR setting.
- Isolated local Caddy v2.11.7 harness (actual Caddyfile HTTP portion, temporary mock API, host loopback only): untrusted spoof → 403 with upstream `http`; ordinary HTTP → 403/`http`; trusted single `https` → 204/`https`; duplicate and comma-chain → 400 before upstream. Harness container/process/temp files were removed. No running services, volumes, certificates, or `.env` were changed.
- Caddy global trust is set by `HTTPS_INGRESS_PROXY_CIDRS`; the empty Compose default is a deny-all proxy list. Operators must set whitespace-separated CIDRs only for actual ingress proxies; API `CREDENTIAL_TRUSTED_PROXY_CIDRS` remains unchanged.
- Rollback boundary: revert the HTTPS-01 work-unit commit to remove only Caddy ingress trust/guard, its compose environment, contract assertions, and this tracker update. Direct TLS behavior, API credential allowlist, mTLS, LAN certificate/bootstrap behavior remain unchanged.
- Work-unit commit: `98fd010c99674481e7c6ee28efa407fda065e2a7` (`fix(proxy): trust configured HTTPS ingress peers`). Parent-owned native review remains outside this worker's scope.

## Objective
A deployment and agent-install strategy that works for most environments (LAN by IP or hostname,
behind any TLS reverse proxy such as Cloudflare Zero Trust / nginx / Traefik, and no-browser
cases), instead of fixing a single scenario. The SSH password must never be stored anywhere.

## Verified findings (2026-10-16)
- SSH itself is not installed "via HTTPS": the OpenVMS server dials SSH. HTTPS protects the root
  SSH password on its way from the browser to the API. The web dialog blocks submit unless
  `location.protocol === "https:"` (`apps/web/src/routes/Servers.tsx:835`), and the API rejects
  install/update/uninstall POSTs without TLS (`credentialInstallHTTPS`, `internal/api/router.go:139-176`).
- The password is not persisted: it is cleared after the job (`internal/provision/agent_install_service.go:255`).
- The API already accepts `X-Forwarded-Proto: https` from `CREDENTIAL_TRUSTED_PROXY_CIDRS` peers
  (default: the web container). Caddy (`deploy/docker/Caddyfile`) has no `trusted_proxies`, so it
  overwrites the proxy's `X-Forwarded-Proto: https` with `http`: through a Cloudflare tunnel to
  port 8000 the browser is on https but the API answers `secure_transport_required`.
- Why self-signed HTTPS fails on fresh deploys:
  1. `HTTPS_SITE_ADDRESS` / `default_sni` default to a developer IP `10.1.1.24`
     (`docker-compose.yml:195`, `Caddyfile`).
  2. The certificate must be bootstrapped manually (`deploy/docker/bootstrap-local-tls.sh <ip>`);
     without it the compose bind mount fails and the web container does not start.
  3. One certificate covers a single IP literal; multiple IPs, DHCP changes or hostnames break it,
     and every browser must trust it manually.

## Proposed strategy (to validate)
1. Any TLS reverse proxy: Caddy honors `X-Forwarded-Proto` only from operator-configured proxy
   CIDRs (env, empty by default); the API keeps its own peer allowlist.
2. Direct LAN access: replace the manual per-IP certificate with Caddy's internal CA generated on
   first start and persisted in a volume, issuing certificates for whatever IP/hostname is used
   (restricted to private ranges, e.g. on-demand with an `ask` endpoint). Operator trusts the root
   CA once (download from `http://<host>:8000/ca.crt`). No bind mount, so deploy never blocks.
3. Always-works fallback: one-time install command with a short-lived token
   (`curl .../install/<token> | sudo bash`), no SSH password in the browser at all.
- Keep agent <-> server mTLS (machine-to-machine, no operator friction).

## Open questions / research spike first
- Can Caddy issue on-demand internal-CA certificates for IP-literal clients that send no SNI?
  (Otherwise: detect host IPs at install time and pre-issue for them.)
- Whether to remove the 8443 listener and self-signed bootstrap entirely after the above lands.

## Next step
Resolve the pending LAN endpoint/certificate product choice (HTTPS-02) before changing direct-LAN TLS behavior. The trusted ingress proxy fix is complete; agent mTLS and existing certificate bind/bootstrap behavior are unchanged.
