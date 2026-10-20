# Compose TLS bootstrap

## Goal

Make a first `docker compose up -d` create the local HTTPS and agent gRPC TLS files required by the stack.

## Checklist

- [x] Add a one-shot Compose service that creates or validates local TLS material before API starts.
- [x] Mount TLS directories so Compose can create dependent containers before the bootstrap runs.
- [x] Make Caddy serve the HTTPS certificate produced by that bootstrap.
- [x] Remove host-side TLS generation from `make install` and document automatic Compose setup.
- [x] Update the Compose CI job to exercise automatic TLS generation.
- [ ] Commit, open a PR, and merge after CI passes.

## Acceptance

- A fresh Compose startup creates `deploy/docker/local-tls/{tls.crt,tls.key,agent-grpc/tls.crt,agent-grpc/tls.key}` before API bind mounts are created.
- Invalid or missing `OPENVMS_HTTPS_SITE_IPV4` fails with a clear Compose configuration error.
- Existing matching certificates are validated and preserved by the existing bootstrap scripts.
