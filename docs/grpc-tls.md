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
- It serves only what agents need, today the node `Heartbeat`. User-facing methods are not registered there. Persisting heartbeats is a later change.
- The agent's server id comes from the certificate (URI SAN `spiffe://openvms/tenant/<tenant>/server/<server>`), never from the request. A `Heartbeat` whose `node_id` is another server is `PermissionDenied`.

### Revocation and validity

Every call, not only the handshake, is checked against `agent_certificates`: the serial must be recorded, the fingerprint, tenant and server must match the certificate, the current time must be inside the recorded validity window, and `revoked_at` must be empty or still in the future (a certificate superseded by a renewal keeps working until then, see below). A refusal is `Unauthenticated` with a single generic message (the reason is logged by the API); a failed lookup is `Unavailable`, so the call fails closed and the agent retries.

There is no cache: revoking takes effect on the next call (an RPC already running finishes). A revoked agent can keep its TCP connection open but cannot use it. Revoking is a database operation today (`agentca.PgRepo.RevokeCertificate` for one serial, `RevokeServerCertificates` for every live certificate of a server; both also end a grace period at once); an admin endpoint is a follow-up.

### Certificate renewal

`AgentService.RenewCertificate(csr_pem) -> certificate_pem, ca_pem` is served on the agent listener only. The caller is the client certificate it presents (it must pass the checks above, so a revoked or expired certificate cannot renew): the server id and tenant of the new certificate come from that certificate, never from the request, and the CSR's subject and SANs are ignored. The CSR is validated (`agentca.ValidateCSR`, at most 8192 bytes) and signed with the loaded CA.

- **Minimum age.** A certificate can be renewed only after half of its lifetime has passed (`not_before + (not_after - not_before) / 2`, database clock). Earlier requests fail with `FailedPrecondition` ("certificate renewal not allowed yet") and write nothing. Agents renew at two thirds, so normal operation is unaffected; the limit stops a looping or stolen client from minting certificates at will.
- **Supersede with grace.** In the same transaction that records the new certificate and its audit row (`SERVER_AGENT_CERT_RENEWED`, target `server_agent`, with the serials and how many certificates were superseded), every other live certificate of the server is set to be revoked 10 minutes from now (`agentenroll.SupersedeGrace`), unless it already ends sooner. During that grace period the old certificate still authenticates, so an agent that lost the response can retry, and connections that still use it keep working while the agent reconnects; afterwards it is rejected like any revoked certificate. A server therefore has the newest certificate plus those still inside their grace period, never an unbounded chain.
- **Compromised key.** Revoke the whole server (`agentca.PgRepo.RevokeServerCertificates`): it ends every certificate of the server immediately, grace periods included, and so cuts the whole renewal chain. The agent must then enroll again with a new token.

### Rollout

Opt-in and additive, nothing changes until `GRPC_AGENT_ADDR` is set:

1. Keep (or set) the server TLS variables and set `GRPC_AGENT_ADDR`, then restart the API. Publish the port only after this.
2. Enroll each agent and move it to the agent port. Existing agents on `GRPC_ADDR` keep working until migrated.
3. To roll back, unset `GRPC_AGENT_ADDR`.
