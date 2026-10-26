# Camera editor rework: General tab, ONVIF capabilities popup, Provision LPR link check
Branch: one branch per task off main (user preference from servers-screen-rework: one PR per task). Delivery strategy: ask-on-risk. Test runner: `pnpm --dir apps/web exec vitest run <path>`, `pnpm --dir apps/web typecheck|lint`, `go test ./internal/<pkg>/...`.

## Specs
S1. Default tab: "por defecto al enetrar en la camara quiero que estemos en general".
S2. Panels: "vamos a mejorar los paneles de configuracion de las camaras".
S3. ONVIF popup: "GENERAL: tenemos toda la configuracion de la camara como cuadros, streams, britage etc, todos los datos informativos de ONVIF quiero que lo dejemos en un popup que se desplega desde un boton capacidades onvif".
S4. General fields: "agregamos Streams de entrada a genereal, switch de camara habilitada arriba de todo, audio, LPR, RECONOCIMIENTO FACIAL, marca de tiempo, transcripcion de audio si esta activo el audio, Tiempo de mejor imagen y tipo de camara, genrica o LPR".
S5. Provision LPR camera: "tambien quiero que trabajemoms en la camara Helv01 que es Provision LPR para mejorar y chequear la conexion REST de la camara a nuestro agente al igual que las analiticas y el test quiero que funcione".
Related, from servers-screen-rework S9 (T9): "en el editor de cmaras tomemos el que tenemos hoy para editar cada camara como popup y vamoms a agregarle una vista de la web de la camara como lo tiene NX Witness que si hay cosas que no podemos configurar via onvif lo hacemos por ahi".

Facts found by the explorer (L2) that shape the work:
- The editor is the full page `/cameras/$cameraId/frigate` (`FrigateCameraConfig.tsx`), not a popup; it opens on the first Frigate schema section (`enabled`), not on General (`activeSection = active ?? sections[0] ?? GENERAL`, line ~201).
- `CameraGeneralForm` saves OpenVMS-only data (name, enabled, quality, location, tags); `CameraOnvifPanel` and the ONVIF facts in `CameraIdentity` hold the informational ONVIF data.
- Most requested fields are Frigate sections (`enabled`, `ffmpeg.inputs`, `audio`, `lpr`, `face_recognition`, `timestamp_style`, `audio_transcription`, `best_image_timeout`); `Camera.lpr` and `Camera.face_recognition` in OpenVMS are read-only mirrors of Frigate; there is no real "camera type" field, only tags.
- Provision LPR: the Configurator cannot change the camera (manual instructions only); success is declared when a keepalive arrives from the registered IP; there is no "test" button; routes `provision-lpr/*` are not in the OpenAPI.
- There is no camera called "Helv01": helv01 is a server. Its Provision cameras are Cementerio (I6-340IPEN-MVF-V5), Escuela (I6-240LPRN-MVF2, tags lpr+provision) and Ingreso Sur (I6-320LPR-MVF2).

Assumptions (not asked by the user; confirm or drop):
- A1. The top "camera enabled" switch writes both OpenVMS `cameras.enabled` and Frigate `enabled`, so disabling a camera also stops Frigate from processing it.
- A2. "Camera type" generic | LPR is a real choice in General that turns the Frigate `lpr` section on and tags the camera; no new database column.
- A3. The Provision LPR "test" observes: it registers the camera, waits for a keepalive from its IP and reports pass/fail with last seen and counters; it cannot push configuration into the camera.

## Tasks
- [ ] C1 (S1) Open the camera editor on General.
- [ ] C2 (S3) Move the ONVIF informational data into an "Capacidades ONVIF" popup opened from a button in General.
- [ ] C3 (S4, A1, A2) General gets the enabled switch on top, input streams, audio, LPR, face recognition, timestamp, audio transcription (only with audio on), best image time and camera type.
- [ ] C4 (S5, A3) Provision LPR camera: check the REST link camera -> agent, make the analytics and a real "Probar conexión" work, and run it on the real camera.
- [ ] C5 (S9) Camera web view inside the editor (spike first). Moved here from servers-screen-rework T9.

## Log
L1 (2026-10-26, user, verbatim): "vamos a trabajar en la edicion de camaras, por defecto al enetrar en la camara quiero que estemos en general

vamos a mejorar los paneles de configuracion de las camaras

GENERAL: tenemos toda la configuracion de la camara como cuadros, streams, britage etc, todos los datos informativos de ONVIF quiero que lo dejemos en un popup que se desplega desde un boton capacidades onvif
agregamos Streams de entrada a genereal, switch de camara habilitada arriba de todo, audio, LPR, RECONOCIMIENTO FACIAL, marca de tiempo, transcripcion de audio si esta activo el audio, Tiempo de mejor imagen
y tipo de camara, genrica o LPR tambien quiero que trabajemoms en la camara Helv01 que es Provision LPR para mejorar y chequear la conexion REST de la camara a nuestro agente al igual que las analiticas y el test quiero que funcione"
L2 (2026-10-26, explorer map): see "Facts found by the explorer" above; code references are in the editor file and the Provision files named there.
