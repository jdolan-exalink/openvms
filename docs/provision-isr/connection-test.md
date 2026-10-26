# LPR connection test

The camera editor has a "Probar conexión" card for LPR cameras. It calls
`POST /api/v1/cameras/{cameraId}/lpr/test` (operation `testCameraLpr`). The test only
observes: it never registers the camera, never writes to the agent and never creates plate
reads. It needs permission to view the camera and `servers.manage` on its server.

The request body is optional: `{ "wait_seconds": 0..30 }` (default 0, anything else is a 400).
With a value above 0, a Provision test polls the agent every 2 s until a keepalive newer than the
call arrives or the time runs out. The cameras send a keepalive about every 20 s, so the web card
waits up to 20 s.

The answer lists `checks` (`pass`, `fail`, `warn`, `skip`), `hints` and the counters `observed`.
`passed` is true when no check is `fail`; warnings do not fail the test.

## Which engine is tested

| Camera | Engine |
|---|---|
| Tag `provision` or `provision_isr`, or a Provision ONVIF manufacturer/model | `provision`: the camera's integrated analytics call our agent over REST |
| Tag `lpr` or the Frigate LPR mirror flag, without the above | `frigate`: Frigate's own engine reads the plates |
| Neither | `none`: one `skip` check, "La cámara no es LPR" |

## Provision checks

The chain is camera, then the server's agent (`/SendKeepalive`, `/v1/provision-lpr/event`),
then central ingestion.

| Check | Meaning | If it fails |
|---|---|---|
| `agent_configured` | The server has a registered agent. | Install or register the agent on the server. |
| `agent_reachable` | OpenVMS can talk to the agent. | Check that the agent is running and that the OpenVMS server reaches its address and port. |
| `receiver_listening` | The agent's Provision receiver answers its status route. | Update the agent: older versions have no receiver. |
| `registered` | The camera is in the agent registry (by id or address). | Run "Registrar cámara y preparar configuración", then enter the values in the camera. The test never registers it for you. |
| `enforcement` | Warns when the agent accepts requests from any address. | Turn on the registry enforcement once the camera is registered. |
| `keepalive_recent` | The last keepalive is within 60 s. | In the camera's web interface, check the server address, port 7419 and the keepalive path, and that the camera can route to the agent. |
| `event_seen` | The agent received at least one plate reading. A warning means the camera is connected but no plate was read yet. | Drive a vehicle past the camera, or check that the camera's LPR analytic is enabled. |
| `central_ingestion` | The worker pulled the agent queue in the last 2 minutes without error, with no lost events and no large backlog. | Check that the worker service runs and that the agent's event queue route works. |

The hints repeat the address the camera must call, built from the server address and agent port
7419: `/SendKeepalive` for keepalives and `/v1/provision-lpr/event` for readings.

## Frigate checks

| Check | Meaning | If it fails |
|---|---|---|
| `frigate_lpr_enabled` | The camera's effective Frigate configuration has `lpr.enabled`. | Enable LPR for the camera in its Frigate configuration. |
| `capability` | The Frigate server reports LPR support. | Enable `lpr` globally in Frigate and restart it. |
| `reads_recent` | There are plate reads for the camera in the last 24 h. Zero is a warning. | Drive a vehicle past the camera and test again. |

The test cannot push settings into the camera: the manufacturer documents no remote way, so the
values are entered by hand in the camera's web interface. See [setup](setup.md) and
[real camera run](real-camera-run.md).
