# One-command deploy and Frigate 0.18 face train parsing

## Objective
Make a deploy of the local compose stack one reproducible command, and show date, label and score of Frigate 0.18 face train attempts.

## Problem and why
- Deploys are done by hand (pull, backup, rollback tags, build, up, health check) and images report `version: dev, commit: unknown`. The user wants new solutions to deploy and work in one step.
- Live check of Frigate `Casa` (0.18.0-77a66e7, read-only, authorized by the user 2026-10-18): train files look like `1792014454.926961-ctk7qb-1792014467.366228-unknown-0.webp`. `parseTrainFile` (`internal/faces/train.go:54`) parses the timestamp as an integer, so every 0.18 attempt has no seen time, label or score in `/faces` Train.

## Scope / tasks
- [x] DFT-01 — Parse fractional Unix timestamps in face train file names (0.17 integer form keeps working). Route: inline (one function + its test).
  - RED: `go test -run TestParseTrainFile ./internal/faces/` failed: `1792014454.926961-ctk7qb-1792014467.366228-unknown-0.webp: not parsed`.
  - GREEN: `strconv.ParseFloat` for the attempt time (`internal/faces/train.go:63`); unit tests pass, `go vet`, golangci-lint 0 issues, `go test -tags integration -run Train ./internal/faces/ ./internal/api/` ok.
- [ ] DFT-02 — `make deploy`: preflight (clean tree on the default branch, fast-forward pull), protected Postgres dump, rollback tags of current api/worker/web images, build with version/commit, `docker compose up -d`, wait for `/health/ready`, print the rollback command; fail fast at any step. Route: delegated direct (writer trigger: Makefile + script + docs).

## Constraints and checks
- Strict TDD (user global config). Go runner: `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test ./internal/faces/...`. Shell: `sh -n`, `shellcheck` if available, `make -n deploy`.
- No remote access beyond the authorized read-only check of `Casa`.

## Progress
- 2026-10-18: branch `feat/deploy-and-face-train-018` from `main` `be86882`.

## Next step
DFT-02 through one writer.
