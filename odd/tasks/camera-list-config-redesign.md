# Camera list as a tree and a consolidated camera configuration

## Header
Task document: `odd/tasks/camera-list-config-redesign.md`.
Objective: turn the camera list into a Site > Server > Camera tree with a left filter rail, and regroup the
per-camera configuration into fewer, graphic sections (Nx Witness as inspiration).
Branch: `feat/cameras-tree-and-config-redesign`. Delivery: two work units, one PR (reviewable slices), then merge.
Test runner: `cd apps/web && npx vitest run <paths>`; `npx tsc --noEmit`. Visual reference: the published mockup
("Maqueta cámaras OpenVMS"), local copy at `/tmp/claude-0/-root-openvms/d5b0993b-df5b-4114-ade9-b4f57cf7c445/scratchpad/ref/mockup.html`.

## Specs

- **S1** — "mejoremos el listado de cammaras, cuando estan todas lo pongamos modo arbol para facilitar el encuentro
  de las mismas": with every server shown the list is a tree Site > Server > Camera (collapsible, search finds a
  camera inside it). With one server selected it is that server's flat list.
- **S2** — "en el nombre del servidor poner si tiene o no agente y version": each server row says whether it has an
  agent and which version (no agent / agent vX / outdated), from the server's `agent_status`, `agent_version`,
  `agent_last_seen_at`.
- **S3** — "quita Descubrir cámaras ONVIF y Probar y detectar": both sections disappear from the camera list.
- **S4** — "filtro de servidores llevalo a la izquierda y mejora la estetica": filters live in a left rail (search,
  server, status, agent), stacked above the list on narrow widths.
- **S5** — "si la resolucion o forma de pantalla no entra el boton de config no hay modo de llegar": the row's
  configuration button is reachable at every width (no horizontal overflow that hides it).
- **S6** — "quitemos la patrala configurar frigate y solo dejemos el engranaje": the "Configurar Frigate" button
  becomes a gear icon (accessible name `Configuración de <cámara>`), same route `/cameras/$cameraId/frigate`.
- **S7** — "clasificacion fina vamoos a llamarlo Analiticas OpenVMS y lo ponemos en el titulo y luego solo dejamos el
  switch de on / off": the column title is "Analíticas OpenVMS"; each row keeps only the on/off switch (accessible
  label kept). Same behavior as the current fine-classification switch.
- **S8** — "no mostremoos las camaras virtuales como birdeye o totems": virtual cameras (Birdseye; the synthetic
  camera inventory creates per server, `remote_name` "birdseye") are hidden by default, with a switch to show them.
- **S9** — "en la configuracion de camaras quiero que mejoremos la cantidad de sub tabs de config avanzados
  unbificando y quitando lo no necesario y que este duplicado": the per-camera configuration goes from 8 tabs plus
  15 Avanzado entries to grouped sections: Resumen, Video, Grabación, Detección y zonas, Analíticas OpenVMS,
  PTZ y fisheye, Conexión, Avanzado (collapsed accordions). Duplicates are removed; nothing that is configurable
  today becomes unreachable.
- **S10** — "mejoremos la estetica por algo mas grafico": left navigation grouped as in the mockup, a visual Resumen
  (preview and health tiles from existing data), one-card-per-stream Video, module cards for Analíticas OpenVMS,
  the existing single save/pending-changes bar. Assumption: no new backend; a weekly recording calendar is NOT
  built (Frigate has no such schedule) and zones keep their existing editor.
- **S11** — "al finalizar vamos con los test y crear PRs para luego comitear y mergear": tests for the new behavior,
  a PR, then merge.

## Tasks

- T1 (S1-S8) — Camera list. Route `apps/web/src/routes/Cameras.tsx`. INLINE/DELEGATED: delegated writer (parallel unit).
- T2 (S9-S10) — Camera configuration regrouping. Route `apps/web/src/routes/FrigateCameraConfig.tsx`,
  `apps/web/src/lib/cameraTabs.ts`, `apps/web/src/components/camera/**`. Delegated writer (parallel unit).
- T3 (S11) — Integrate, run the full web suite and typecheck, open the PR, merge. DONE: PR #295 merged as `7a64b33`.

## Log

- **L1** (user, verbatim): "quiero que mejoremos el listado de cammaras, cuando estan todas lo pongamos modo arbol
  para facilitar el encuentro de las mismas en el nombre del servidor poner si tiene o no agente y version, quita
  Descubrir cámaras ONVIF y Probar y detectar , filtro de servidores llevalo a la izquierda y mejora la estetica,
  tambien fijate que si la resolucion o forma de pantalla no entra el boton de config no hay modo de llegar, quitemos
  la patrala configurar frigate y solo dejemos el engranaje clasificacion fina vamoos a llamarlo Analiticas OpenVMS y
  lo ponemos en el titulo y luego solo dejamos el switch de on / off, no mostremoos las camaras virtuales como
  birdeye o totems, en la configuracion de camaras quiero que mejoremos la cantidad de sub tabs de config avanzados
  unbificando y quitando lo no necesario y que este duplicado investiguemos las config de NX Witness para tomar como
  ejemplo y si podemos mejoremos la estetica por algo mas grafico y todas las funciones que podamoms mejorar quiero
  que lo plantiemos, si te parece armame un ejemplo de como puede quedar"
- **L2** (user, verbatim): "me encanta vamos con todo al finalizar vamos con los test y crear PRs para luego comitear y mergear"
- **L3** (context): PR #294 (`feat/camera-web-live-origin`) carries the earlier camera web work. Servers already
  expose `agent_version`, `agent_status`, `agent_last_seen_at`; Birdseye is created by `internal/inventory/servers.go`.
  No backend change planned. "Outdated" agent = version lower than the highest version seen among the loaded servers
  (no fleet-wide expected version exists in the API).
