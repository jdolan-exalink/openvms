# Provision-ISR: setting up, checking and undoing the integration

Stage 1 of the Provision-ISR plugin: a camera is registered on its server's agent from the camera
configuration screen, the agent tells it from any other host on the network, and the screen shows when
it really reports in. Events still stay on the agent: forwarding them to central is stage 2. What the
cameras send is documented in `capture.md`; what they are known to do, in `capability-matrix.md`.

## Set up a camera

1. Open the camera in **Configuración de cámaras**. Provision-ISR cameras are recognised by their ONVIF
   manufacturer or model, or can be marked by hand ("Forzada manualmente").
2. In the Provision-ISR section press **Registrar cámara y preparar configuración**. OpenVMS:
   - registers the camera's address on the server's agent (the camera's ONVIF address, or the one you
     type), so the agent can identify it by that address;
   - returns the values to enter in the camera (server address defaults to the host of the Frigate
     server, port 7419, heartbeat path `/SendKeepalive`, event path `/v1/provision-lpr/event`,
     20 s heartbeat).
3. Enter those values in the camera's web interface: **Configuración > Red > Configuración de
   Servidor > HTTP POST / API**. This step is manual: the manufacturer documents no way to set it
   remotely, so OpenVMS neither tries nor sends the camera's credentials anywhere (earlier versions
   posted to a guessed CGI and reported success; they no longer do).
4. When the camera starts sending keepalives the section shows it online and the result changes to
   **la integración está verificada**. Until then it stays on the manual steps, whatever the
   registration did.

The camera's address must be the one it connects **from**, as the agent sees it. A camera behind NAT,
or reaching the agent through a proxy, is seen with another address; the agent ignores forwarding
headers on purpose (they are chosen by the sender and would let any host pose as a camera).

## Exigir cámaras registradas (enforcement)

By default the agent keeps accepting data from any camera on the network, as before the registry
existed, so upgrading cuts nothing. The section **Cámaras permitidas en el receptor** lists the
addresses that sent data without being registered. Register the cameras you want to keep, then press
**Exigir cámaras registradas**: from then on requests from any other address get HTTP 403, are kept
as evidence (capture reason `unregistered`, when captures are on) and appear as *rechazada*. Turning it
off again does not need a confirmation.

If the registry file on the agent cannot be read the agent refuses every camera instead of accepting
everyone, and the screen says so; repair or delete the file on the host
(`$OPENVMS_AGENT_STATE_DIR/provision-registry.json`) and restart the agent.

## Agent routes

Camera-facing routes carry no token (cameras cannot send one): `/SendKeepalive`, `/v1/provision-lpr/event`
and the other event paths. Everything else needs the agent token as a bearer: `/v1/provision-lpr/status`
(it lists camera addresses and plates; it used to be open), `/v1/provision-lpr/registry...`,
`/v1/provision-lpr/captures...`, `/v1/provision-lpr/events...` (the queue central collects, see
`ingestion.md`) and `/v1/provision-lpr/configure`.

Central API (cookie or API token, needs `servers.manage` on the camera's server for changes):

```text
POST   /api/v1/cameras/{cameraId}/provision-lpr/configure   register + prepare + verify
DELETE /api/v1/cameras/{cameraId}/provision-lpr/register    unregister
GET    /api/v1/servers/{serverId}/provision-lpr/registry    registry and unregistered sources
PUT    /api/v1/servers/{serverId}/provision-lpr/enforce     {"enforce": true|false}
GET    /api/v1/servers/{serverId}/provision-lpr/ingestion   how far central got collecting readings
```

An agent older than this stage answers 404 to the registry routes; the API turns that into
`agent_outdated` and the screen asks to update the agent. Registering does not work until it does, and
enforcement cannot be turned on.

## Check it

- Camera section: online badge, keepalive count and last event come from the agent's status.
- `GET /v1/provision-lpr/status` on the agent (with the token) returns `cameras` and, when the registry
  exists, `registry` with `enforce`, the registered cameras and `unregistered_seen`.
- Agent log: `provision-isr camera registered`, `provision-isr request from an unregistered address
  refused`, `provision-isr registry enforcement changed`.

## Undo it

- One camera: **DELETE** the registration (or remove it from the registry). If enforcement is on the
  camera stops being accepted; turn enforcement off first to keep it flowing unidentified.
- All of it: turn enforcement off, then delete `provision-registry.json` from the agent's state
  directory and restart the agent. The agent goes back to accepting every camera and identifying
  none, exactly as before this stage.
- Captures: `OPENVMS_AGENT_PROVISION_CAPTURE=off` and delete `provision-captures/` (see `capture.md`).
- Whole stage: deploy the previous version. Nothing changed in the database and no migration ran; the
  only state is the two files above on each agent.
