# Pending PR Closeout

## Goal

Complete the closed but unmerged ONVIF and Faces work, resolve the current integration-test failure, and deploy the verified main branch to the local Compose stack.

## Delivery slices

- [ ] ONVIF imaging options (#169): rebase against current main, preserve the contract source of truth, regenerate clients, verify protocol and API tests, then merge.
- [ ] ONVIF edge snapshots (#170): rebase against current main, regenerate clients, verify edge/API behavior and security bounds, then merge after #169 if shared changes require stacking.
- [ ] Faces inbox and grouping (#191/#192, with #193 already merged into #192's head): restore as a stacked chain, rebase against current main, regenerate contracts, verify Go and web behavior, then merge in dependency order.
- [ ] Alarm integration failures: determine root cause from CI and reproduce locally; fix only the alarm behavior or test setup implicated by evidence.
- [ ] Main verification: Go lint/unit/vulnerability checks, integration suite, web checks, Docker image builds, Compose smoke.
- [ ] Local deployment and smoke test after all merge commits; retain the deploy script's backup and health-check behavior.
- [ ] Hailo hardware inference acceptance: verify only if the local host exposes a Hailo device and matching driver; otherwise record the host requirement as the remaining external check.

## Constraints

- Keep contract/generated files in sync; edit OpenAPI source and regenerate generated outputs.
- Preserve tenant isolation, face crop privacy, permission checks, and edge snapshot host/size/timeout restrictions.
- Respect the accepted size exceptions for the Faces slices; split ONVIF work into cohesive PR slices where possible and keep generated snapshots intact.
- Do not deploy until changes are merged and the integration failure is understood.
