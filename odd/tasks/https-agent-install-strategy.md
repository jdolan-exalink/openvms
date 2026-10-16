# HTTPS and agent SSH install strategy (backlog — deferred until after alarms phase 2)

Status: NOT STARTED. User decision (2026-10-16): close alarms phase 2 first; pick this up at the end.

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
Research spike on Caddy internal CA + IP/no-SNI, then a concrete plan and task list.
