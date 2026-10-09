# User-triggered HTTPS redirect for Servers SSH actions

Provide an explicit way for an operator on HTTP to reopen the Servers screen over the configured HTTPS port before using SSH credentials. Do not auto-redirect or weaken TLS verification.

## Why and constraints

The install and update dialogs currently show only an HTTPS-required notice when `location.protocol` is HTTP. Compose maps `WEB_HTTPS_PORT` to the web container's port 443, but does not pass the published port into Caddy or the browser. A hard-coded default would fail when an operator configures another port.

Keep this independent from the near-limit ONVIF ledger. No API endpoint, password submission over HTTP, external host, automatic navigation, TLS bypass, query/hash preservation, or credential preservation is in scope. Migration 34 remains deployed and forward-only. The feature implementation itself did not deploy; a separately authorized local web-only rollout is recorded below.

## Accepted design

Expose the configured published HTTPS port as a non-secret web-container runtime setting and a same-origin Caddy endpoint available on both listeners. The endpoint returns only the numeric port as `text/plain`, with `Cache-Control: no-store`. Frontend validates a strict decimal port in `1..65535`; missing or invalid configuration disables/refuses redirect rather than guessing.

Both HTTP dialogs show a localized button, not an automatic redirect. Only an explicit click fetches the runtime setting without credentials and with redirects rejected, clears password input/ref state, then navigates to `https:` on the same hostname and pathname with query, fragment, and URL userinfo removed. Never bypass the browser's certificate validation; a self-signed certificate may still require normal operator trust.

## Work units

| ID | Task and acceptance | Checks |
|---|---|---|
| HTTPS-REDIRECT-01 | Pass the effective `WEB_HTTPS_PORT` (default `8443`) into the Caddy runtime; serve a same-origin, no-store port endpoint on both HTTP/HTTPS listeners. No API route or secret in response. Verify default and non-default compose configuration and endpoint placement. | `docker compose config --quiet`; `WEB_HTTPS_PORT=9443 docker compose config --format json` contract check; Go deployment/Caddy contract tests. Caddy validation is check-only; do not start containers. |
| HTTPS-REDIRECT-02 | Add explicit localized redirect buttons to both SSH dialogs on HTTP. Click-only behavior uses strict runtime-port validation and same-origin URL construction; strip query/hash/userinfo and clear password state before navigation. Preserve normal HTTPS behavior and TLS verification. | RED first in `Servers.test.tsx`; assert no automatic redirect, default-independent custom port, same host/path, no query/hash/credential carryover, invalid/unavailable endpoint refusal, no credential POST/storage. Then focused Vitest, typecheck, full web suite, `git diff --check`. |

## Route, TDD, and delivery

- Route: delegated direct; multi-file UI/Caddy/config change requires reading and writing across independent contexts. Trigger evidence: compose/Caddy runtime configuration, React dialogs, locales, and tests are separate nontrivial files.
- Strict TDD: enabled by `AGENTS.md`; runner is `(cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx)`. Add observed failing regressions before implementation, then GREEN and REFACTOR.
- Forecast: approximately 150–300 authored changed lines. Delivery strategy: `ask-on-risk`; approved chain strategy: `stacked-to-main`. No push or PR is authorized by this task.
- No host/camera/SSH request or production/local deployment is needed. Runtime proof is `N/A`; config checks and mocked browser tests are sufficient.

## Progress

- [x] HTTPS-REDIRECT-01 — runtime-configured Caddy port endpoint. Added `OPENVMS_WEB_HTTPS_PORT` from `WEB_HTTPS_PORT` and a shared HTTP/HTTPS Caddy endpoint; Go contract test observed RED before config/code and GREEN after.
- [x] HTTPS-REDIRECT-02 — localized explicit redirect controls and tests. Added explicit English/Spanish/Portuguese buttons, bounded strict runtime-port fetch, and same-host/path HTTPS navigation with secret/query/hash removal. Vitest observed RED before implementation and GREEN afterward.

### Verification evidence

- RED: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose -run TestWebComposeAddsPinnedHTTPSIngressWithoutBreakingHTTPOrProxyTrust` failed because the new runtime setting was absent.
- RED: `(cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx)` failed on the missing redirect controls (3 initial cases; 4 after adding the close-abort regression).
- GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose` and `GOCACHE=/tmp/openvms-go-build-cache go test -race -count=1 ./deploy/agent/compose` passed.
- GREEN: `docker compose config --quiet` passed; with `WEB_HTTPS_PORT=9443`, compose JSON checks confirmed host port `9443` and `OPENVMS_WEB_HTTPS_PORT=9443`.
- GREEN: isolated check-only Caddy validation with `OPENVMS_WEB_HTTPS_PORT=9443` returned `Valid configuration`; no service was started or restarted.
- GREEN: `(cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx)` passed (43 tests); `(cd apps/web && pnpm typecheck)` passed; `(cd apps/web && pnpm test)` passed (120 files, 880 tests); scoped ESLint on the five changed web files and `git diff --check` passed.
- During implementation verification, no deployment, browser credential submission, SSH, database, camera, or live listener test was performed. The later separately authorized listener checks are recorded below. HTTPS still depends on normal browser trust of the configured certificate.

### Separately authorized local web rollout

- User authorization covered only the local web container. No API, PostgreSQL, agent, SSH, or camera service was rebuilt, restarted, or queried.
- Pre-deploy `docker compose config --quiet` passed. The web container used image `sha256:0b2206de60245b84f45e6487b04fffdd788dbe277e12b3eaac11931b008027ac`; it was preserved as `openvms-web:rollback-pre-https-redirect-1a48309`. API and PostgreSQL container IDs before/after remained `79005e5bfa87...` and `6209555b37bf...` respectively.
- `docker compose build --pull=false web` succeeded; `docker compose up -d --no-deps web` recreated only `openvms-web-1`. Active web image is `sha256:62845cd9016c56f248e8f5af0b437c4bf684532a6916fc248bee66dff65204e0`; runtime `OPENVMS_WEB_HTTPS_PORT=8443`.
- With `curl --noproxy '*'` and without `-k`, HTTP `http://10.1.1.24:8000/health/live` and HTTPS `https://10.1.1.24:8443/health/live` both returned 200. The port endpoint returned body `8443`, `text/plain`, and `Cache-Control: no-store` on both listeners; HTTPS used `--cacert deploy/docker/local-tls/tls.crt` and also validated successfully.
- Served `Servers-D4TBPMId.js` contained the redirect action and runtime endpoint strings. No authenticated browser interaction or SSH credential submission was performed.
- Rollback boundary: restore the web service to `openvms-web:rollback-pre-https-redirect-1a48309` and recreate only `web`; retain API/PostgreSQL and data as-is. No rollback was needed.

## Rollback

Remove the added web runtime setting, Caddy endpoint, dialog button/helper, translations, contract/UI tests, and this task document. Preserve the existing HTTP/HTTPS listeners and existing TLS ingress guard.
