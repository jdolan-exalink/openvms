# HTTPS and agent SSH install strategy

Status: ACTIVE — trusted HTTPS ingress is complete; HTTPS-04 auto-provisioned appliance certificate work is authorized.

## Tracking
- Branch: `feat/https-agent-ingress`, based on `feat/alarms-phase2-rules` at `8809c418746720c097e6f177fe20092b55133ab6`.
- Route: delegated direct; strict TDD enabled by user/session configuration.
- Runners: `GOCACHE=/tmp/openvms-go-build-cache go test -race -count=1 ./deploy/agent/compose ./internal/platform/config ./internal/api`; `GOCACHE=/tmp/openvms-go-build-cache go vet ./deploy/agent/compose ./internal/platform/config ./internal/api`; `docker compose config --quiet`; isolated full Caddy runtime harness; `git diff --check`.
- Authored change: approximately 469 lines (additions plus deletions, no generated files); this is one cohesive deployable behavior with tests/docs, so it was not artificially sliced or code-golfed. The 400-line heuristic is advisory. Delivery remains ask-on-risk with the cached stacked-to-main strategy; no PR has been created.
- Delivery: `ask-on-risk` (default); if slicing is needed, use previously user-selected `stacked-to-main`. No PR creation authorized.
- Constraints: no remote operations, deployment/restart, SSH/tunnel changes, actual `.env` edits, broad private-range trust, or mTLS changes. Explicit user authorization supersedes prior “LAN certificate strategy unsettled/no LAN certificate changes authorized” constraints only for HTTPS-04 auto-provisioning. No arbitrary Host-header issuance, token installer, or alarms work.

## Tasks
- [x] HTTPS-01 — Configure explicit operator-controlled Caddy ingress proxy CIDRs (empty/default deny); preserve HTTPS forwarding only for a trusted immediate peer and enforce a single exact `https` protocol value. Add regression tests for trusted, untrusted/spoofed, duplicate/multi-hop, and ordinary HTTP inputs. Route: delegated direct; strict TDD.
- [x] HTTPS-02 — Choose fixed operator-configured SAN host list and persisted self-signed leaf cert for appliance-style direct access; retain Caddy and external-cert override. No dynamic Host-header issuance or automatic host-interface discovery.
- [x] HTTPS-03 — Close with focused/broader verification, isolated runtime evidence, work-unit commit, and tracker/mirror sync.
- [x] HTTPS-04 — Auto-generate and persist a self-signed HTTPS leaf on first start; renew on startup when SAN set changes or expiry threshold is reached; validate/atomically install pair; preserve explicit external cert mode and actionable invalid config failures. Configure multiple IPv4/IPv6/DNS SANs with localhost fallback, preserve no-SNI IP TLS, HTTP and 8443 behavior. Route: delegated direct; strict TDD true (user/session configuration); delivery ask-on-risk with cached stacked-to-main.
- [x] HTTPS-05 — Bound TLS generation lock waiting so an abandoned lock cannot hang startup forever; fail with safe, actionable recovery guidance without stealing a live lock. Remove duplicate DNS-label validation and document manual recovery. RED → GREEN → REFACTOR. Parent requested before native candidate freeze.

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

## Baseline findings before HTTPS-01 (2026-10-16)
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

## HTTPS-04 implementation decisions and evidence
- Keep Caddy and issue one persisted self-signed leaf from the explicit `OPENVMS_HTTPS_SANS` comma-separated DNS/IPv4/IPv6 list. Default SANs are `localhost,127.0.0.1,::1`; legacy `OPENVMS_HTTPS_SITE_IPV4` remains a listener override and fallback SAN input.
- Require `OPENVMS_HTTPS_DEFAULT_SNI` (default `localhost`) to be present in that SAN set so no-SNI TLS has a matching default certificate identity.
- Do not discover host addresses from inside Docker and do not issue for request Host headers. Operators must list addresses/names in advance and still establish browser trust/name validation themselves.
- The generator uses OpenSSL installed in the web image, a private named volume, a generation lock, staged cert/key files, and atomic `current` symlink replacement. It reuses matching certs with more than 30 days remaining; SAN changes or expiry threshold rotate at next container start, not continuously while Caddy runs. Tests cover concurrent starts, near-expiry rotation, DNS label bounds, invalid IPv4/IPv6, and default-SNI membership.
- Generated material is an RSA-3072 self-signed server leaf (`CA:FALSE`, serverAuth only); key mode 0600, cert mode 0644. External cert/key host file overrides remain supported and must both be supplied, parse, and match or startup fails.
- Keep HTTP port 8000, HTTPS 8443, `default_sni`, ingress proxy trust/anti-spoof guard, API TLS gate, and mTLS behavior unchanged.
- Strict-TDD RED included missing configured SAN/volume/script support, malformed SAN rejection, and sourced-entrypoint lock leakage; an additional RED caught OpenSSL's default `CA:TRUE` on `req -x509`. GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -race -count=1 ./deploy/agent/compose ./internal/platform/config ./internal/api`; `GOCACHE=/tmp/openvms-go-build-cache go vet ./deploy/agent/compose ./internal/platform/config ./internal/api`; `docker compose config --quiet`; `git diff --check`.
- Isolated full runtime harness used actual changed Caddyfile and entrypoint in `caddy:2-alpine` with only owned loopback ports 18443/18080 and a temporary TLS directory. HTTPS and HTTP `/.well-known/openvms-https-port` returned 200/`18443`; `openssl s_client -connect 127.0.0.1:18443 -noservername` returned a cert validating IP SAN 127.0.0.1; DNS SAN `web.test` validated; SHA-256 fingerprint was identical after container restart. Container and temporary TLS directory were removed.
- No-SNI proof depends on the generated multi-SAN certificate plus Caddy `default_sni`; the test intentionally connects without SNI to the IP literal.
- Rollback boundary: revert the HTTPS-04 work-unit commit to remove auto-generation/volume/SAN configuration, entrypoint, docs, and focused tests; HTTPS-01 proxy trust implementation and API behavior remain unchanged.
- HTTPS-04 work-unit commit: `5f67a90` (`feat(https): provision persistent self-signed certificates`); 431 additions / 38 deletions, 469 changed lines. No PR, push, or deployment.
- HTTPS-05 RED: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose -run '^TestAutomaticWebTLSBootstrapFailsActionablyOnAbandonedGenerationLock$' -v` failed because the pre-existing lock caused the script to outlive the test's 3-second context deadline.
- HTTPS-05 GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -race -count=1 ./deploy/agent/compose -run '^TestAutomaticWebTLSBootstrap' -v` passed all 11 auto-TLS tests, including abandoned-lock failure, concurrent startup, and normal lock release. `sh -n deploy/docker/prepare-web-tls.sh` and `git diff --check` passed.
- HTTPS-05 uses a 30-second default lock bound (configurable from 1 to 300 seconds for operators/tests), and never steals/removes an existing lock. Timeout reports the lock path and directs operators to stop the web container and verify no startup process is using the volume before manual removal. README documents the same safe recovery. Removed the duplicate DNS-label validation command.
- HTTPS-05 work-unit commit: `3f1de34f52477eadd97394bec96974de6c359389` (`fix(https): bound certificate lock wait`).
- Native R4-001 correction: use `mktemp` for unique staging directories; while holding the generation lock, remove only abandoned real directories matching `.generation-*`, excluding the lock and current symlink target. RED: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose -run '^TestAutomaticWebTLSBootstrapRecoversFromAbandonedGenerationDirectory$' -v` failed because a precreated same-PID stage caused `mkdir: File exists`. GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -race -count=1 ./deploy/agent/compose ./internal/platform/config ./internal/api`; `GOCACHE=/tmp/openvms-go-build-cache go vet ./deploy/agent/compose ./internal/platform/config ./internal/api`; `docker compose config --quiet`; `sh -n deploy/docker/prepare-web-tls.sh`; `git diff --check` passed. Parent retains the existing review binding and must perform the corrected-candidate STATUS/capture; this worker did not invoke native review commands.

## Next step
Parent-owned risk assessment/native review for the normalized work-unit commit. No deployment, restart of current project services, remote operation, PR, or push was performed.
