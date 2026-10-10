# gRPC control channel TLS

The API serves its gRPC control channel on `GRPC_ADDR` (default `:9090`). It can be served over server-side TLS: the channel is encrypted and clients verify the server certificate. Client certificates (mTLS) are not required or checked; mTLS and agent identity are a separate feature.

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

The server certificate is always verified; there is no option to skip verification. The certificate must carry a SAN matching the dial host (or `TLSServerName`).

## Rollout

A TLS server rejects plaintext clients and a plaintext server rejects TLS clients, so order matters:

1. Provision the certificate and key and set `GRPC_TLS_CERT_FILE` / `GRPC_TLS_KEY_FILE` on the API, then restart it. Plaintext clients stop connecting from this point.
2. Switch each edge agent and desktop client to TLS (`OPENVMS_CONTROL_TLS=true`, plus the CA file when the certificate is not publicly trusted).

To avoid a gap, switch clients during the same maintenance window as the server. Rolling back is the reverse: unset the server variables and clients' TLS settings.

Publishing port 9090 beyond the host (for example in compose) is not part of this change and should happen only after TLS is enabled.
