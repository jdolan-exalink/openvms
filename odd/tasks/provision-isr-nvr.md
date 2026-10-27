# Provision-ISR NVR support (Phase 1: read-only NVMS web API, server kind, import)

Objective: add Provision-ISR NVRs to OpenVMS as an alternative to Frigate servers. Plan: `docs/plans/provision-isr-integration.md`.
Branch: `feat/provision-nvr-phase1`. Delivery strategy: `ask-on-risk`. Runner: `go test ./internal/...`, `go test -tags integration`, `pnpm --dir apps/web test`.
Status: plan written, evidence gathered from a real unit, implementation not started.

## Specs

- S1. "Quiero agregar a **OpenVMS** soporte nativo para NVRs de **Provision-ISR**, especialmente equipos basados en la plataforma **Ossia**." and "La integración debe diseñarse como una integración real de VMS/NVR y NO simplemente como una forma de obtener URLs RTSP."
- S2. "un administrador pueda agregar un NVR Provision-ISR a OpenVMS ingresando: IP o hostname, Puerto, Usuario, Contraseña" and OpenVMS discovers model, firmware, channels, names, health, storage and capabilities as far as the device allows.
- S3. "NO asumir ni inventar endpoints, protocolos, payloads o estructuras propietarias de Provision-ISR." Unknown support is marked `UNKNOWN`, `REQUIRES REAL DEVICE TEST`, `REQUIRES SDK DOCUMENTATION` or `REQUIRES PAYLOAD SAMPLE`.
- S4. "No bloquear toda la integración porque una capacidad avanzada no esté disponible." Capabilities are detected per device and the UI shows only what is confirmed.
- S5. "Reutilizar" the existing abstractions; "NO crear infraestructura paralela innecesariamente." No massive refactors.
- S6. "Credenciales: utilizar secrets vault existente; nunca mostrarlas en logs; nunca devolverlas en API; no persistirlas dentro de RTSP URL." TLS verification is never disabled globally.
- S7. Each recorder channel becomes an OpenVMS camera that remembers its parent recorder and channel ("camera parent_device = provision_nvr, provision_channel = 3"). Several NVRs per site without id collisions.
- S8. Robust reconnection: states `online | degraded | offline | auth_failed | unsupported`, backoff, no reconnect storms.
- S9. "FASE 1 ... Implementar: device; discovery; channels; main/substream; audio; PTZ fallback; capabilities; UI básica." Adapted by evidence (see L2): Phase 1 delivers the server kind, read-only discovery, capabilities, channel import, health and the add-device UI; video, PTZ and events wait for real evidence.
- S10. "Documentación: docs/integrations/provision-isr.md" and a compatibility table with states `confirmed | partial | experimental | unsupported | unknown`.
- S11. "Primero generá: docs/plans/provision-isr-integration.md con el diseño adaptado al código REAL existente."
- S12. Test device for validation: a recorder on the local network, credentials supplied by the owner in the session. They are never written to the repository, memory, logs or fixtures.

## Tasks

- [x] T0 (S11) Plan document written from the real code and a read-only probe of the unit. Route: inline (one read-only explorer for the code map). Commit: -
- [ ] T1 (S4, S5) Migration: `frigate_servers.kind` (`frigate` default, `provision_nvr`), sqlc, `ServerView.Kind`, OpenAPI `Server.kind` (additive). Existing behavior unchanged. Risk: item 1 (migration). Route: inline.
- [ ] T2 (S2, S3, S6, S8) `internal/provisionnvr`: NVMS web API client (login, session, redacted errors, safe XML, size cap), typed read-only queries, sanitized fixtures from the real unit, fake NVMS server for tests. Route: inline.
- [ ] T3 (S2, S4) Capability model and `Detect`, API `POST /servers/provision/detect` (read-only, `servers.manage`). Risk: item 2 (credentials, new endpoint taking a password). Route: inline.
- [ ] T4 (S2, S7, S8) Create/sync a `provision_nvr` server with channel selection; channels as cameras; kind-aware health; kind-aware camera removal. Risk: item 1/4 (shared sync and health paths). Route: inline.
- [ ] T5 (S2, S4) Web: add-device flow, capability rendering, channel selection, Provision panel. Route: inline.
- [ ] T6 (S10, S12) `docs/integrations/provision-isr.md`, compatibility table, read-only run against the real unit recorded. Route: inline.

## Log

- L1 (2026-10-27, owner): "quiero que arranquemos con el soporte a NVRs Provision-ISR como opcion a servidores frigate o NVRs, te paso un promt completo para que tomes de referencia" (the full prompt, 44 sections, is the reference), and "y te aparte te paso para que hagas test http://192.168.68.200 usuario admin pass [given in the session, not recorded here] para que testes el funcionamiento de la misma".
- L2 (2026-10-27): evidence changed the premise of Phase 1. The test unit is a TVT NVMS-9000 platform recorder (`NVR8-16400AN(1U)`, web client 20220914.01). ONVIF did not answer (device reports `switch=false`; enabling it is a settings change, not done), the RTSP server (Digest) rejected four candidate paths with 401 (format UNKNOWN), port 6036 speaks an undocumented binary protocol, the API Server feature is switched off. What does work and is documented by the device's own web client is the NVMS XML web API on port 80. Phase 1 is therefore built on that read-only API instead of ONVIF/RTSP, and video, PTZ and events are deferred with their blockers in the plan. Login: `sha512_hex(MD5_hex_UPPERCASE(password) + "#" + nonce)`; the first attempt with a lowercase digest failed, one corrected attempt succeeded; no further failed logins were made.
- Next step: T1.
