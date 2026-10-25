# Servers screen rework: version colours, degraded reasons, NICs, fewer buttons, unified edit panel
Branch: one branch per slice off main (see Tasks). Delivery strategy: ask-on-risk. Test runner: `go test ./internal/<pkg>/...`, `pnpm --dir apps/web test -- --run <path>`, `pnpm --dir apps/web typecheck`.

## Specs
S1. Agent version colour: "el color de la version si esta actualizado no sea amarilla como esta mostrando ahora y sea amarillo cuando tenemos una actualizacion y verde si esta ok".
S2. Degraded reason: "si el servidor esta degradado tenemos que mostrar el problea en el resumen del servidor".
S3. Network interfaces: "las placas de red quiero que mostremos solo LAN y CCTV y en el de helv01 estamoms viendo mas que esas 2".
S4. Buttons: "quiero que acomodemos los botonoes que tenemos muchos inecesarios y solo dejemos uno para actualizar agente que sea un proceso interno del agente y no se haga mas via ssh, otro para desinstalar el agente, backup de configuracion de agente y frigate y de editar servidor".
S5. Edit server: "en editar servidor vamos a tener todos los datos de frigate, agente y editor de la config frigate, tenemos que ahi ver tambien datos relevamntes del servidor como estado de agente, frigate, cantida de camaras, boton para editar las camaras del servidor, configuracion de grabacion, y todo los datos relevantes y soporte nuevos que tenemos".

Assumptions (not asked by the user; confirm or drop):
- A1. "Update agent" uses the existing `updateServerAgent` operation (HTTP push to the agent's `/v1/update`, no SSH password).
- A2. Frigate's virtual `birdseye` camera does not count towards "degraded".
- A3. An agent whose binary status is unknown (not verified) shows a neutral colour, not yellow or green.

## Tasks
- [x] T1 (S1, A3) Version colour from `binary_status`: green current, yellow update_available, neutral otherwise. Route: inline.
- [x] T2 (S2, A2) Persist the list of degraded reasons and show it in the server summary. Route: inline.
- [ ] T3 (S3) Show only the LAN and CCTV interfaces using the agent's `role`. Route: inline.
- [ ] T4 (S4, A1) Row actions reduced to: update agent (HTTP), uninstall agent, backup, edit server. Route: inline.
- [ ] T5 (S4) Backup of agent and Frigate configuration. Route: inline. Needs scope decision.
- [ ] T6 (S5) Edit server as one detail panel. Route: inline.

## Log
L1 (2026-10-25, user, verbatim): "ya muestra la version correcta, quiero que el color de la version si esta actualizado no sea amarilla como esta mostrando ahora y sea amarillo cuando tenemos una actualizacion y verde si esta ok,  si el servidor esta degradado tenemos que mostrar el problea en el resumen del servidor, las placas de red quiero que mostremos solo LAN y CCTV y en el de helv01 estamoms viendo mas que esas 2, quiero que acomodemos los botonoes que tenemos muchos inecesarios y solo dejemos uno para actualizar agente que sea un proceso interno del agente y no se haga mas via ssh, otro para desinstalar el agente, backup de configuracion de agente y frigate y de editar servidor: en editar servidor vamos a tener todos los datos de frigate, agente y editor de la config frigate, tenemos que ahi ver tambien datos relevamntes del servidor como estado de agente, frigate, cantida de camaras, boton para editar las camaras del servidor, configuracion de grabacion, y todo los datos relevantes y soporte nuevos que tenemos"
L2 (2026-10-25, explorer map, evidence):
- Version is rendered in `AgentMonitor` (apps/web/src/routes/Servers.tsx:1591-1657); `hasUpdate` (:1609) = `binary_upgrade_available || binary_outdated`; `binary_upgrade_available` is true whenever a bundled binary exists and no digest comparison clears it (internal/provision/agent_status.go:65-69), so unverified agents show yellow `text-warn` (:1616).
- Degraded is set in internal/inventory/health.go:196-250 (enabled offline camera, storage >90%, detection stalled); only the stall writes `last_error`; birdseye is not excluded (synthesized in servers.go:79-93). UI shows one string `last_error` at Servers.tsx:115.
- NIC `role` (cameras|monitoring|lan) is computed in internal/agent/interfaces.go:232-253 and carried in `network_interfaces`; UI ignores it (`isLanInterfaceName`, Servers.tsx:1699).
- UI update goes over SSH (`updateServerAgentOverSSH`); HTTP `updateServerAgent` (POST /api/v1/servers/{id}/agent/update, provision_handlers.go:228) already exists and is unused by the UI. Agent `/v1/update` needs scope `update`, verifies ELF, SHA-256 and version, and arms a rollback.
- Uninstall exists only over SSH; no agent or Frigate config backup/download exists; `ServerFrigateConfigModal` is the real config editor, `EditServerModal` (:2084) is a plain form.
L3 (2026-10-25, T2 done): the health poll now stores `problems` (cameras_offline with names, storage_high with used percent, detection_stalled) in the server stats and exposes them as `Server.problems`; BirdEye no longer counts as an offline camera (A2 applied); the Servers row lists the reasons instead of the raw `last_error` when `problems` is present. UI tests were written after the render code, so their RED was not observed; the Go helper tests were observed RED (undefined symbols) before the implementation.
