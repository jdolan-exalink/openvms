# Provision-ISR: running the plugin against a real camera (E0.4)

This is the step-by-step procedure to learn what a real Provision-ISR camera (the I6-340 named in the
brief, or any other model) sends, and to turn that into evidence for `capability-matrix.md`. Nothing
here guesses a protocol: every step either records what the camera does or checks something OpenVMS
already claims.

Plan and requirements: `odd/tasks/provision-isr-plugin.md`. How captures work: `capture.md`. Setting a
camera up from OpenVMS: `setup.md`.

## What you will bring back

1. Exact model, firmware and what ONVIF reports (section 2).
2. The raw requests the camera sends: at least two real LPR readings, a keepalive, and one event of
   every other analytic the camera can produce (section 5).
3. A screenshot or text of the camera's HTTP POST screen and of each analytic's setup screen.
4. How the camera behaves when the receiver is down or restarts (section 7).
5. The filled results sheet at the end of this file.

## Before you start

| Need | Why |
|---|---|
| The agent on the server that can reach the camera is updated to a build that includes the capture routes (#221) | Without it there is nothing to capture with. The registry (#222) and the camera screen (#223) are needed for the optional registration in section 4 and for the status screen. |
| A shell on that server with root (or access to `/etc/openvms/agent.token`) | The capture routes need the agent token; the camera has no token to send. |
| Camera address, an admin login for its web interface, and the manual (Provision-ISR Smart Sight Series User Manual, sections 4.4.2, 4.5.2, 4.5.3 and Appendix I) | The only documented way to point the push at a server is the camera's web interface. |
| A way to produce a plate reading (a real vehicle or a printed plate in the camera's detection area) | The LPR event format has never been captured. |
| NTP on the camera and the server | Timestamps decide how events correlate with video. |

Two warnings:

- **Capture-all mode answers 200 to a POST on any path from any host.** Use it for a short window on a
  network where only the cameras can reach the agent, and turn it off afterwards (section 8).
- **Captures hold real plates and may hold faces.** They are personal data: keep them on the agent,
  do not paste them in tickets or chats, and delete them when the run is finished unless they are
  anonymized (section 6).

Set the agent address and token once; the commands below use them:

```bash
AGENT=http://127.0.0.1:7419            # the agent's own listener, as seen from the server
TOKEN=$(sudo cat /etc/openvms/agent.token)
H="Authorization: Bearer $TOKEN"
```

If the agent only has its TLS listener, use `https://` with the agent's certificate options instead.

## 1. Check the agent

```bash
curl -s -H "$H" $AGENT/v1/provision-lpr/status        # {"cameras": [...]} ; with the registry also "registry"
curl -s -H "$H" $AGENT/v1/provision-lpr/captures       # 404 "captures are not enabled" means an old agent
```

Expected: both answer. A 401 means the token is wrong; a 404 on `/status` means the agent is older than
the receiver itself.

## 2. Identify the camera and read what ONVIF says

1. In OpenVMS open the camera in the camera configuration screen and run the ONVIF check with the
   camera's ONVIF address and login. (API: `POST /api/v1/cameras/{cameraId}/onvif/check`.)
2. Write down: manufacturer, model, firmware version, serial number, profiles, the list of services and
   the **advertised analytics** with their topics (screen section "Analíticas anunciadas por la
   cámara (ONVIF)"; raw topics in `onvif_capabilities.supported_analytics`).
3. Cross-check model and firmware in the camera's own web interface (System information). If they
   disagree, the web interface wins and the ONVIF identity is noted as unreliable.

Advertised does not mean working: a topic is only counted as confirmed when a real event arrives
(section 5).

## 3. Turn capture-all on

```bash
sudo sed -i '/^OPENVMS_AGENT_PROVISION_CAPTURE=/d' /etc/openvms/agent.env
echo 'OPENVMS_AGENT_PROVISION_CAPTURE=all' | sudo tee -a /etc/openvms/agent.env >/dev/null
sudo systemctl restart openvms-agent
journalctl -u openvms-agent -n 20 --no-pager | grep -i "capture-all"     # the warning proves the mode is on
```

An agent update replaces the agent but should keep `agent.env`; re-check the line after any update.

## 4. Point the camera at the agent

1. Optional but recommended: in the OpenVMS camera screen press **Registrar cámara y preparar
   configuración**. It registers the camera's address on the agent and shows the values to enter.
   (Capture does not need it; the registry only identifies the camera.)
2. In the camera's web interface open **Network > Advanced > HTTP POST** and set: Enable on, Protocol
   Type **API**, Server Address = the agent host's address as the camera sees it, Server Port **7419**,
   Heartbeat interval 20 s. Save. Take a screenshot.
3. Within about a minute check that keepalives arrive:

```bash
curl -s -H "$H" $AGENT/v1/provision-lpr/status | python3 -m json.tool | head -30
```

Expected: the camera appears with `online: true` and a growing `keepalive_count`. Keepalives are not
captured (their format is already known); this is only to see that the camera reaches the agent. If
nothing arrives, check the route between the camera and the agent (firewall, VLAN) before going on.

## 5. Make the camera produce events and read them

Do one thing at a time, note the wall-clock time, and after each look at the captures.

| Trigger | What to record |
|---|---|
| A known plate in the LPR area, twice, a few seconds apart, then once after the camera's own dedup window | Path, content type, body format, field names, whether images travel in the request, whether the camera sends both a panoramic and a plate crop, its dedup behaviour. |
| An unknown plate, and a plate on the camera's allow list or block list if you set one | Whether list status appears and in which field. |
| Each other analytic the camera lists (face, person, vehicle, line crossing, intrusion, motion, tamper): enable it in the camera's web interface, trigger it | Whether it sends anything over the HTTP POST at all, to which path, and whether it also appears as an ONVIF topic. |
| Two events at nearly the same time | Whether they arrive as separate requests. |

Read what arrived:

```bash
curl -s -H "$H" $AGENT/v1/provision-lpr/captures | python3 -m json.tool | head -60
ID=<an id from the list>
curl -s -H "$H" $AGENT/v1/provision-lpr/captures/$ID | python3 -m json.tool           # headers, path, content type, size, hash
curl -s -H "$H" -o /tmp/capture.bin $AGENT/v1/provision-lpr/captures/$ID/body        # the original bytes
file /tmp/capture.bin; head -c 600 /tmp/capture.bin
```

Reading the body: for `application/xml` or JSON the text is readable as is; for `multipart/*` the
`boundary` is in the capture's `content_type`, and each part has its own headers and, usually, a
JPEG. Do not edit the originals.

Notes while reading:

- A request that carries a plate and that the receiver understood also raises the agent's own event
  count (visible in `/status` as `event_count` and `last_plate`); one it did not understand is a
  capture with reason `unparsed`. Both are useful; the unparsed ones show where the parser is wrong.
- Look for a stable identity of the camera inside the payload (serial, device ID, MAC). The registry
  can identify a camera by address alone, but a payload identity would be stronger.
- Look for what the camera does with a non-200 answer: the agent always answers 200, so retries
  can only be studied in section 7.

## 6. Make anonymized samples

Real captures stay on the agent. For the repository and for tests:

1. Copy the body and the description of each capture you want to keep as a sample.
2. Replace every plate with a made-up one of the same shape (for example `AB123CD`), every serial,
   MAC and internal address with documentation values (`192.0.2.x`), and every timestamp with a fixed
   one. Keep the structure, field names, order, headers and content type exactly as they were.
3. Replace each image with a tiny valid JPEG of the same name and content type; never keep a real
   vehicle or face.
4. Keep a note of what you changed, then send the samples with the results sheet. Fixtures are
   written from them and committed with tests that pin the real format.

## 7. Behaviour under failure

Each test: do it, trigger an event or wait for a keepalive, note what the camera and the agent do.

| Test | What to observe |
|---|---|
| `sudo systemctl stop openvms-agent` for a few minutes, then start it | Does the camera resume keepalives by itself? How long after? Does it resend events it could not deliver (compare captures after the restart with the readings made while it was down)? |
| Restart the agent during an event | Is the event lost? Does the camera retry it? |
| Disconnect the camera from the network briefly | Does `/status` mark it offline after the heartbeat timeout (60 s by default) and online again afterwards? |
| Send the same reading twice inside and outside the agent's 15 s window | Is the second one suppressed by the agent (event count) while both are captured in capture-all mode? |
| Two cameras at once, if available | Do they keep separate identities in `/status`? |

Stage 2 (a persistent queue and sending events to central) does not exist yet, so two checks from the
brief cannot be made in this run: that events show up in the central LPR, search, Vision AI and
alarms modules, and that the agent recovers pending events after a central outage. Record only what
the camera does.

## 8. Put it back

```bash
sudo sed -i '/^OPENVMS_AGENT_PROVISION_CAPTURE=/d' /etc/openvms/agent.env
sudo systemctl restart openvms-agent
# once the samples are safe, and unless you need the originals:
sudo rm -rf /var/lib/openvms-agent/provision-captures
```

Leave the camera's HTTP POST pointing at the agent if the integration is going to stay; otherwise turn
it off in the camera.

## 9. Fill in the matrix

For each row of `capability-matrix.md` that the run touched:

- Seen working: change **To verify** to **Confirmed** and add the firmware, the date and the capture
  IDs (or the sample file names) to the notes.
- Tested and absent: change it to **Unsupported** only if you did what the manual says to enable it
  and nothing arrived; say what you tried.
- Not tested: leave it as **To verify**.

Add rows for anything the camera sent that the matrix does not list.

## Results sheet

Copy this block into the ticket or the PR and fill it in.

```text
Camera: model ____  firmware ____  serial ____  address ____
ONVIF: profiles ____  services ____  advertised analytics ____
Agent: version ____  host ____
HTTP POST screen: protocol type ____  address/port ____  heartbeat ____  (screenshot attached)

LPR
  endpoint(s) seen: ____
  content type / format: ____
  fields found: plate ____ time ____ confidence ____ direction ____ list status ____ camera id ____ other ____
  images in the request: panoramic yes/no  plate crop yes/no  (format ____)
  camera-side dedup observed: ____ s
  captures: ____ (ids)

Other analytics (one line each: sent over POST yes/no, path, ONVIF topic yes/no, captures)
  face: ____   person: ____   vehicle: ____   line crossing: ____   intrusion: ____   motion/tamper: ____

Identity inside the payload (serial / device id / MAC): ____
Failure behaviour: resumes after agent restart ____  resends missed events ____  offline detected in ____ s
Long Polling Port (documented 8080): reachable yes/no, what the manual says it speaks ____ (do not probe beyond the manual)
Surprises: ____
```

After entering the values in the camera, use the "Probar conexión" card in the camera editor to verify each link; see [connection-test.md](connection-test.md).
