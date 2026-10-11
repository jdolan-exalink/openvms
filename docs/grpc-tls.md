# gRPC control channel TLS

The API serves its gRPC control channel on `GRPC_ADDR` (default `:9090`). It can be served over server-side TLS: the channel is encrypted and clients verify the server certificate. Client certificates are not required on this listener. Edge agents can use a separate mutual-TLS listener instead; see [Agent listener](#agent-listener-mutual-tls).

## Server (API)

| Variable | Meaning |
| --- | --- |
| `GRPC_TLS_CERT_FILE` | PEM server certificate (include the intermediate chain if any). |
| `GRPC_TLS_KEY_FILE` | PEM private key for that certificate. |

- Both set: the gRPC server uses TLS 1.2 or newer.
- Neither set: plaintext, and the API logs a warning at startup.
- Only one set: the API fails to start.
- Certificates are read once at startup. Restart the API to pick up a renewed certificate.

## Edge agent

| Variable | Meaning |
| --- | --- |
| `OPENVMS_CONTROL_TLS` | `true` dials `OPENVMS_SERVER_ADDR` / `OPENVMS_CONTROL_ADDR` over TLS. Default `false` (plaintext). |
| `OPENVMS_CONTROL_TLS_CA_FILE` | Optional PEM bundle that signed the server certificate. Empty uses the system roots. |
| `OPENVMS_CONTROL_TLS_SERVER_NAME` | Optional name to verify instead of the dial host (for example when dialing by IP). |

`OPENVMS_CONTROL_TLS_CA_FILE` and `OPENVMS_CONTROL_TLS_SERVER_NAME` without `OPENVMS_CONTROL_TLS=true` stop the agent at startup, so a typo cannot silently leave the channel in plaintext. With TLS enabled, the CA bundle is also loaded at startup: a missing file or one without certificates stops the agent instead of leaving it running without a control channel.

## Desktop SDK (`pkg/client`)

`client.Config` gains `TLS`, `TLSCAFile`, and `TLSServerName` with the same meaning. `TLSCAFile` or `TLSServerName` without `TLS` makes `client.New` return an error.

`client.Config.Insecure` was removed. It was never read, so it implied a secure channel it did not provide. This is a compile-time break for external callers that set it; use `TLS` (and `TLSCAFile` / `TLSServerName` when needed) instead.

The server certificate is always verified; there is no option to skip verification. The certificate must carry a SAN matching the dial host (or `TLSServerName`).

## Rollout

A TLS server rejects plaintext clients and a plaintext server rejects TLS clients, so order matters:

1. Provision the certificate and key and set `GRPC_TLS_CERT_FILE` / `GRPC_TLS_KEY_FILE` on the API, then restart it. Plaintext clients stop connecting from this point.
2. Switch each edge agent and desktop client to TLS (`OPENVMS_CONTROL_TLS=true`, plus the CA file when the certificate is not publicly trusted).

To avoid a gap, switch clients during the same maintenance window as the server. Rolling back is the reverse: unset the server variables and clients' TLS settings.

Publishing port 9090 beyond the host (for example in compose) is not part of this change and should happen only after TLS is enabled.

## Agent listener (mutual TLS)

Edge agents get a unique, revocable identity from the internal agent CA (enrollment: `POST /api/v1/agent/enroll`) and present it on a separate listener. The user and SDK port above is unchanged.

| Variable | Meaning |
| --- | --- |
| `GRPC_AGENT_ADDR` | Listen address of the agent listener, for example `:9443`. Empty (the default) disables it. |

- It reuses `GRPC_TLS_CERT_FILE` / `GRPC_TLS_KEY_FILE` as its server certificate. `GRPC_AGENT_ADDR` without them makes the API fail to start.
- TLS 1.2 or newer, and a client certificate signed by the agent CA is required (`RequireAndVerifyClientCert`). The CA is created or loaded at API startup; a CA or master-key problem stops the API.
- It serves only what agents need: the node `Heartbeat` and `AgentService.RenewCertificate`. User-facing methods are not registered there.
- The agent's server id comes from the certificate (URI SAN `spiffe://openvms/tenant/<tenant>/server/<server>`), never from the request. A `Heartbeat` whose `node_id` is another server is `PermissionDenied`.
- Each accepted heartbeat is stored (see [Heartbeat persistence](#heartbeat-persistence)).

### Revocation and validity

Every call, not only the handshake, is checked against `agent_certificates`: the serial must be recorded, the fingerprint, tenant and server must match the certificate, the current time must be inside the recorded validity window, and `revoked_at` must be empty. A certificate's first accepted call also activates it (see [Certificate renewal](#certificate-renewal)). A refusal is `Unauthenticated` with a single generic message (the reason is logged by the API); a failed lookup is `Unavailable`, so the call fails closed and the agent retries.

There is no cache: revoking takes effect on the next call (an RPC already running finishes). A revoked agent can keep its TCP connection open but cannot use it. Revoking is a database operation today (`agentca.PgRepo.RevokeCertificate` for one serial, `RevokeServerCertificates` for every live certificate of a server); operators use `POST /api/v1/servers/{serverId}/agent/revoke-certificates` (see below).

### Certificate renewal

`AgentService.RenewCertificate(csr_pem) -> certificate_pem, ca_pem` is served on the agent listener only. The caller is the client certificate it presents (it must pass the checks above): the server id and tenant of the new certificate come from that certificate, never from the request, and the CSR's subject and SANs are ignored. The CSR is validated (`agentca.ValidateCSR`, at most 8192 bytes) and signed with the loaded CA. Recording the certificate and its audit row (`SERVER_AGENT_CERT_RENEWED`, target `server_agent`, with the serial, the previous serial and how many unused successors were replaced) happen in one tenant-scoped transaction.

- **Minimum age.** A certificate can be renewed only after half of its lifetime has passed (`not_before + (not_after - not_before) / 2`, database clock). Earlier requests fail with `FailedPrecondition` ("certificate renewal not allowed yet") and write nothing. Agents renew at two thirds, so normal operation is unaffected.
- **Supersede on first use.** Renewing does not revoke the old certificate. The new certificate is recorded with `parent_serial` set and `first_used_at` empty, and the old one keeps working. When the agent first authenticates a call with the new certificate, the listener (in one transaction, with a per-server lock shared with renewals) sets `first_used_at` and revokes every other live certificate of the server, so the old one stops working exactly when the agent proves it has the new one. This happens once per certificate, not per call; a failure to record it makes that call `Unavailable` (fail closed) and the agent retries.
- **Lost response.** If the agent never receives the renewal response it still holds a working old certificate, and renews again. Each renewal revokes the presenting certificate's own unused successors (the pending successors of its earlier attempts), so there is at most one pending successor per presenting certificate, and the agent is never locked out by a lost response. Other unused certificates of the server, such as a fresh re-enrollment, are left alone; whichever certificate is used first revokes the rest.
- **Stolen old key.** Once the legitimate agent has used its new certificate, the old one is revoked, so a copy of the old key can neither call nor renew (a renewal from a revoked certificate is refused as `Unauthenticated`), and it cannot push the legitimate agent's certificate out. Until the agent switches, the old key and the agent are indistinguishable, which is inherent to a renewal chain; the minimum age bounds how often it can be abused.
- **Concurrency.** Renewals and first uses of one server take the same advisory lock, so a pending successor's first call racing a retry from its parent has exactly one winner.
- **Compromised key.** Revoke the whole server (`agentca.PgRepo.RevokeServerCertificates`): it ends every certificate of the server immediately and so cuts the whole renewal chain. The agent must then enroll again with a new token.

Clocks: validity windows are checked against the API process clock; revocation, first use and the minimum age use the database clock.

### Heartbeat persistence

Every accepted `Heartbeat` upserts one row per server in `agent_heartbeats` (migration 00044): `last_seen_at` (the database clock, never the agent's), `version` (at most 64 bytes) and `status` (`unknown`, `online`, `degraded`, `offline` or `maintenance`). The write runs in a transaction scoped to the certificate's tenant, under row-level security, with a 3 second bound; if it fails the heartbeat is `Unavailable` and the agent retries on its next tick. It is one small indexed write per heartbeat per server (every 10 seconds per agent by default); if that ever matters, skip the write when nothing changed and `last_seen_at` is recent.

The server read APIs (`GET /api/v1/servers`, `GET /api/v1/servers/{serverId}`) return `agent_last_seen_at`, `agent_version` and `agent_status`. They are absent until the first heartbeat. `agent_status` is what the agent reported, not liveness: decide whether an agent is still reporting by comparing `agent_last_seen_at` with the current time.

### Rollout

Opt-in and additive, nothing changes until `GRPC_AGENT_ADDR` is set:

1. Keep (or set) the server TLS variables and set `GRPC_AGENT_ADDR`, then restart the API. Publish the port only after this.
2. Enroll each agent and move it to the agent port. Existing agents on `GRPC_ADDR` keep working until migrated.
3. To roll back, unset `GRPC_AGENT_ADDR`.

## Edge agent with mutual TLS

The agent uses mTLS when `OPENVMS_AGENT_GRPC_ADDR` is set; without it nothing below applies and the agent behaves as before.

| Variable | Meaning |
| --- | --- |
| `OPENVMS_AGENT_GRPC_ADDR` | `host:port` of the API's agent listener (`GRPC_AGENT_ADDR`). Enables mTLS and takes precedence over `OPENVMS_SERVER_ADDR` / `OPENVMS_CONTROL_ADDR`, which are then ignored (a warning is logged). |
| `OPENVMS_ENROLL_TOKEN` or `OPENVMS_ENROLL_TOKEN_FILE` | The one-time enrollment token (a file keeps it out of the process environment). Only needed until a certificate is stored; setting both is an error. |
| `OPENVMS_API_URL` | `https` base URL of the API, for the enrollment call. Plain `http` is refused because the token is a secret. |
| `OPENVMS_API_CA_FILE` | Optional PEM bundle trusted for the API's HTTPS certificate. Empty uses the system roots. There is no skip-verify option. |
| `OPENVMS_AGENT_STATE_DIR` | State directory, default `/var/lib/openvms-agent`. Credentials live in `<state>/agent-mtls/`. |
| `OPENVMS_CONTROL_TLS_CA_FILE`, `OPENVMS_CONTROL_TLS_SERVER_NAME` | Verify the agent listener's server certificate (the API's `GRPC_TLS_CERT_FILE`), as for the control channel. TLS is implied; `OPENVMS_CONTROL_TLS=false` is an error. |

The token, API, state and token-file variables without `OPENVMS_AGENT_GRPC_ADDR` stop the agent at startup, so a typo cannot silently leave it unenrolled.

### Enrollment

1. An operator creates a token for the server (`POST /api/v1/servers/{serverId}/agent/enroll-token`, valid 15 minutes, single use) and gives it to the agent.
2. On start, if `<state>/agent-mtls/` holds no certificate, the agent generates an ECDSA P-256 key locally, sends only a CSR with the token to `POST {OPENVMS_API_URL}/api/v1/agent/enroll`, and checks that the returned certificate matches its key, carries an agent identity and chains to the returned CA. The private key never leaves the agent.
3. It stores `key.pem` (0600), `cert.pem`, `ca.pem` and a hash of the spent token (`enroll-token.sha256`) by writing temporary files and renaming them, so a crash never leaves a half-written file. A renewal interrupted between its two renames is finished on the next start.

Stored credentials always win: a token left in the environment after enrollment is ignored, and a corrupt state directory is an error, never a reason to enroll again (the token is gone). A rejected token (unknown, used or expired, all answered `401`) stops the agent with a message asking for a new token; the token is never logged. The one exception is a stored certificate that has already expired together with a token that differs from the spent one: the operator issued a fresh token to recover the agent, so it enrolls again.

The agent then dials `OPENVMS_AGENT_GRPC_ADDR` with its certificate and heartbeats with the server id from that certificate (the configured `OPENVMS_NODE_ID` is ignored).

### Renewal

The agent renews once two thirds of the certificate's lifetime have passed (about day 20 of 30; the server accepts renewals from half, so the agent never trips the minimum age), over mTLS with `RenewCertificate`, using a fresh key. A `FailedPrecondition` answer (too early, for example after a clock correction) is not an error: the agent logs it and retries later with the same backoff. The new certificate is verified (same identity, matches the new key, chains to the CA) and persisted before the agent starts using it. The old certificate stays valid until the new one is first used, so there is no deadline to switch: the worker reconnects on its next heartbeat tick (10 seconds by default), and that first call revokes the old certificate. If the response is lost, the agent still has its old certificate, keeps heartbeating with it and renews again after the usual backoff; the server replaces the unused successor of the first attempt. Only if the old certificate itself expires without a successful renewal is a new enrollment token needed.

A failed attempt is retried with exponential backoff (30 seconds up to 15 minutes), and the loop re-reads the clock at least hourly so a suspended host or corrected clock does not delay renewal. If the certificate expires without a successful renewal it can no longer authenticate, so renewal cannot work any more; the agent logs an error saying so on every attempt and at startup. Recovery is a new enrollment token and a restart (the expired-certificate exception above).

### Rollout for agents

1. Enable the agent listener on the API (see above) and publish its port.
2. For each agent, create a token and set `OPENVMS_AGENT_GRPC_ADDR`, `OPENVMS_ENROLL_TOKEN` (or the file), `OPENVMS_API_URL` and the control CA settings, then restart the agent. It enrolls on first start and moves to the agent port.
3. To roll an agent back, unset `OPENVMS_AGENT_GRPC_ADDR` and restart; it returns to `OPENVMS_SERVER_ADDR`. Its issued certificates stay in the database until they expire or are revoked. Uninstalling an agent should also remove `<state>/agent-mtls/` (the SSH install and uninstall wiring is a later change).

### Revoking an agent

`POST /api/v1/servers/{serverId}/agent/revoke-certificates` (needs `servers.manage`) revokes every certificate of the server's agent that is not revoked yet and returns `{"revoked": N}`. The listener refuses those certificates from the next call and the agent cannot renew, so the whole renewal chain ends and the agent has to enroll again with a new token. It is audited (`SERVER_AGENT_CERTS_REVOKED`, target `server_agent`, with the count), runs in one transaction scoped to the caller's tenants, and a second call returns zero. It does not delete pending enrollment tokens; create a new token (which replaces the old one) to re-enroll.

### Enrollment rate limit

`POST /api/v1/agent/enroll` is public, so it is limited per client IP: a burst of 10 requests, then one every 6 seconds; beyond that the API answers `429` with `Retry-After`. The key is the address recorded for the audit log (the right-most `X-Forwarded-For` entry when `TRUST_FORWARDED_FOR` is on, so a client cannot dodge the limit with its own header). The limiter is in memory and per API process, bounds its own table, and does not protect the 256-bit tokens (they are not guessable); it bounds the CSR and database work an anonymous caller can cause. Renewal is bounded separately by the minimum age.

## SSH install with mutual TLS

When the API runs the agent listener and knows how agents reach it, the SSH agent install (`POST /api/v1/servers/{serverId}/agent/install`) enrolls the new agent for mutual TLS by itself. Without these settings installs behave exactly as before.

| API variable | Meaning |
| --- | --- |
| `AGENT_GRPC_PUBLIC_ADDR` | `host:port` of the agent listener (`GRPC_AGENT_ADDR`) as the agent host reaches it. Becomes the agent's `OPENVMS_AGENT_GRPC_ADDR`. |
| `AGENT_API_PUBLIC_URL` | `https` base URL of the API as the agent host reaches it. Becomes `OPENVMS_API_URL`. |
| `AGENT_GRPC_CA_FILE` | Optional PEM file on the API host with the CA that signed `GRPC_TLS_CERT_FILE`; copied to the agent as `OPENVMS_CONTROL_TLS_CA_FILE`. Empty makes agents use their system roots. |
| `AGENT_GRPC_SERVER_NAME` | Optional name agents verify on that certificate (`OPENVMS_CONTROL_TLS_SERVER_NAME`). |
| `AGENT_API_CA_FILE` | Optional PEM file with the CA of the API's HTTPS certificate, copied as `OPENVMS_API_CA_FILE`. |

`AGENT_GRPC_PUBLIC_ADDR` and `AGENT_API_PUBLIC_URL` are set together and need `GRPC_AGENT_ADDR`; the API cannot know its own public address, so there is no default. The CA files are read at startup and a bad path stops the API.

Flow of an install with these settings:

1. The install job creates a one-time enrollment token for the server (the same call as `enroll-token`: it needs `servers.manage` and `servers.config.secrets` and is audited as `SERVER_AGENT_ENROLL_TOKEN_CREATED`), right before the SSH run because the token lives 15 minutes. If it cannot be created the job fails before touching the host.
2. Over SFTP the installer writes `/etc/openvms/agent-enroll.token` (mode 0600; the token is never in `agent.env`, a command line or a log), the CA files (0644) and these lines in `/etc/openvms/agent.env` (0600): `OPENVMS_AGENT_GRPC_ADDR`, `OPENVMS_ENROLL_TOKEN_FILE`, `OPENVMS_API_URL`, `OPENVMS_AGENT_STATE_DIR=/var/lib/openvms-agent` and, when set, the CA and server-name lines. All values are validated before any SSH connection.
3. The agent starts, enrolls once, keeps its key, certificate and CA under `/var/lib/openvms-agent/agent-mtls/` and heartbeats on the agent port. The token file can stay; stored credentials always win over a spent token.

Uninstall removes the service, `/var/lib/openvms-agent/agent-mtls/`, the enrollment token file and `/etc/openvms`. It does not revoke the server's certificates; use the revocation endpoint (or `RevokeServerCertificates`) when an agent is decommissioned.

Gradual rollout: only installs started after the settings are in place enroll. Agents already installed keep running on the control channel (`OPENVMS_SERVER_ADDR`) until moved. To move one by hand, create a token for its server and add `OPENVMS_AGENT_GRPC_ADDR`, `OPENVMS_ENROLL_TOKEN_FILE`, `OPENVMS_API_URL` (and the CA settings) to its `agent.env`, then restart it; to roll one back, remove `OPENVMS_AGENT_GRPC_ADDR` and restart. The update flow (`update-ssh`) does not change an agent's control settings.
