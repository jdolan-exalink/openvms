# Named SSH server installation

## Objective and authorization
Remove the manual SSH SHA-256 fingerprint form fields and request a server display name for fresh installations. User explicitly accepted the unverified first-contact impersonation risk. Local implementation only; remote installation remains excluded. Preserve all pre-existing dirty changes and the previous no-commit constraint. User owns functional testing.

## Scope and constraints
English technical artifacts, existing localized UI in en/es/pt. Keep explicit fingerprint validation available for compatible API clients. Automatic trust must be an explicit API opt-in, not silent fallback when a fingerprint is missing. Do not describe first contact as verified. Capture the first observed key and require consistency across this installation's subsequent SSH connections if supported by the existing flow. No remote probes or credentials.

## Route and delivery
Delegated direct: UI, OpenAPI/generated bindings, provisioning handler/service/SSH and tests are multiple non-trivial files. Estimated 200–350 authored changed lines; generated bindings excluded. Strategy ask-on-risk. No PR, push or commits: preserve previously explicit no-commit constraint in the active feature. RDD enabled; native candidate assessment applies after normalization.

## Tasks
- [x] T1 Implement named installation and explicit automatic first-contact SSH trust; retain pinned-key client compatibility and localized warning. Route delegated (multi-file writer/preparation trigger). Structural readback confirmed propagation; tests deferred to user.
- [ ] T2 Check contract/build consistency; report test limitations and local deployment status. Route delegated (execution trigger). Do not deploy remotely.

## Acceptance and checks
Fresh-host form requests a nonempty name and no fingerprint/confirmation; supplied name reaches CreateServer. Manual fingerprint API clients continue to validate mismatch before password auth. Missing fingerprint without automatic-trust opt-in remains rejected. First-contact automatic trust is clearly disclosed and secrets never logged. Existing import unaffected.

TDD configuration: enabled by AGENTS.md; exact runners: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision ./internal/api ./internal/agent` and `cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx src/i18n/i18n.test.ts --reporter=dot`. User previously requested to perform all testing themselves; automated test execution is deferred under that explicit request, so no RED/GREEN evidence will be claimed. Required build-only check: `cd apps/web && pnpm typecheck`; Go compilation via build. Runtime install harness deferred to user.

## Progress
T1 implemented: `server_name` is required in the new-install form/API and reaches inventory registration; `trust_on_first_use` is explicit and false by default outside this form. Missing fingerprint without opt-in remains rejected; any supplied key is checked even with opt-in. The current flow uses one SSH connection and records the observed fingerprint, without persistent first-contact identity guarantees. English/Spanish/Portuguese warning updated. Old clients omitting newly required `server_name` must update.

Writer regenerated Go/web OpenAPI bindings and ran gofmt; `pnpm typecheck` PASS. Parent and independent verifier observed `git diff --check` PASS. Go build NOT VERIFIED: sandbox denied module cache writes and escalated retries were interrupted without approval. Tests NOT RUN per user-owned testing instruction; no RED/GREEN claimed. New changes NOT deployed; running local containers still contain the preceding version. No remote operations or commits.

## Rollback
Revert only named-installation and auto-trust additions relative to the already-dirty pre-change working tree; preserve prior Servers provisioning, Maps and deployment changes.

## Next step
T2 pending Go build permission and local rebuild/deployment. RDD assessment high/unassessable due to undeclared untracked files; preflight inventory selected relevant provisioning files and tracker, excludes standalone HTML and unrelated Maps tracker. Native consent still pending; no review verdict claimed.
