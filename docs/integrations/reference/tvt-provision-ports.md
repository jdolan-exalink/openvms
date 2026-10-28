# Provision-ISR / TVT ports: owner-supplied reference

> **Status of this document.** The section below the line is the text the owner supplied (2026-10-28) as a reference for the Provision-ISR
> integration. It is kept verbatim, in Spanish, as a lead, not as proof. Do not turn a claim from it into a `confirmed` capability until it
> was seen working on a real unit (the rule of `docs/integrations/provision-isr.md`).
>
> ## What our test unit says about it
>
> Unit: `NVR8-16400AN(1U)`, TVT NVMS-9000 platform, read-only checks only.
>
> | Claim in the reference | On our unit |
> |---|---|
> | NVR native port is `6036/TCP` | **Matches.** `queryNetPortCfg` reports `netPort = 6036`, and the port is open. It answers with a binary banner that starts with ASCII `head`. The protocol was not spoken (no SDK documentation), so what it offers is `REQUIRES SDK DOCUMENTATION` |
> | Cameras use `9008/TCP` | **Matches.** `queryNodeList` lists the 7 cameras at port `9008` with protocol `TVT_IPCAMERA` |
> | RTSP on `554` | **Matches.** `queryRTSPServer`: on, port 554, Digest, anonymous off. Stream URLs from ONVIF: `rtsp://host:554/chID=<n>&streamType=main\|sub&linkType=tcp`. Playback not exercised |
> | HTTP on `80`, HTTPS on `443` | HTTP 80 matches. HTTPS: `httpsPort = 443` is configured but `httpsSwitch = false`, so 443 is closed |
> | ONVIF has no fixed port; try `/device_service` | ONVIF answers on the web port 80 at **`/onvif/device_service`** (401 without WS-UsernameToken). `/device_service` returns 400 on this unit. It only worked after the ONVIF switch was enabled and saved |
> | "API Server" option is separate from 6036 | **Matches.** `queryApiServer` reports the API Server switched off; port 6036 is a different service |
> | Other ports the unit reports | `p2pPort 46038`, `posPort 9036` (not used by OpenVMS) |
>
> Facts already used by OpenVMS: the NVMS XML web API on port 80 (login, channels, status, disks) and ONVIF on the same port. The native SDK
> on 6036 is the Phase 4 lead in `ROADMAP271026.md`; it needs the TVT `NET_SDK` documentation (or a captured session) before any code.
>
> The reference's own closing rule applies to OpenVMS: ports are known defaults, never hardcoded; the host, web port and HTTPS are fields the
> user edits; fall back SDK, then ONVIF, then RTSP; never infer a feature from a port number.

---

# Provision-ISR / TVT – Puertos de NVR, DVR y Cámaras IP

## Objetivo

Este documento resume los puertos de red más relevantes al integrar dispositivos **Provision-ISR** y equipos basados en plataforma **TVT** con un VMS, NVR, software de monitoreo o una integración propia.

Está especialmente pensado como referencia para el desarrollo de integraciones como **OpenVMS / CamMesh**, donde necesitamos distinguir entre:

- acceso nativo por SDK,
- video RTSP,
- administración web,
- ONVIF,
- eventos y analíticas,
- descubrimiento e importación automática de canales.

> **Importante:** Provision-ISR utiliza distintas generaciones de hardware y firmware, y varios equipos están basados en tecnología OEM de TVT. Algunos puertos pueden variar según modelo, firmware o configuración del usuario.

---

# 1. Resumen rápido

| Servicio | NVR / DVR | Cámara IP | Uso |
|---|---:|---:|---|
| SDK / protocolo privado | **6036/TCP** | **9008/TCP** en muchos modelos TVT/Provision | Control nativo, login, canales, playback, eventos, configuración |
| RTSP | 554/TCP-UDP | 554/TCP-UDP | Video y audio |
| HTTP | 80/TCP | 80/TCP | Web UI, API HTTP, ONVIF en algunos modelos |
| HTTPS | 443/TCP | 443/TCP | Administración web cifrada |
| ONVIF | Variable | Variable | Descubrimiento, perfiles, video, PTZ, eventos |
| API / eventos HTTP | Variable | Variable | LPR, AI, analytics, callbacks, POST |
| Multicast / discovery | Variable | Variable | Descubrimiento de dispositivos |

---

# 2. Puerto nativo de NVR / DVR

## TCP 6036

En NVR y DVR Provision-ISR, el puerto **6036/TCP** suele corresponder al protocolo privado utilizado por:

- software cliente,
- CMS,
- OSSIA,
- SDK nativo,
- aplicaciones móviles,
- integraciones de terceros.

En dispositivos basados en TVT este protocolo suele estar asociado al SDK denominado:

```text
NET_SDK
```

o a librerías del tipo:

```text
libdvrnetsdk.so
```

## Funciones que puede proporcionar

A través del protocolo nativo del puerto 6036 se pueden encontrar funciones como:

- autenticación,
- información del dispositivo,
- enumeración de canales,
- estado de cámaras,
- obtención de URLs RTSP,
- selección de main stream / sub stream,
- live view,
- snapshots JPEG,
- búsqueda de grabaciones,
- reproducción histórica,
- descarga de grabaciones,
- fecha y hora,
- configuración de video,
- alarmas,
- eventos,
- PTZ,
- reinicio del dispositivo,
- información de capacidades.

Arquitectura conceptual:

```text
OpenVMS
   |
   | TCP 6036
   v
Provision NVR
   |
   +-- CH01
   +-- CH02
   +-- CH03
   +-- CH04
   +-- ...
```

Para una integración VMS, **6036 es particularmente interesante** porque puede permitir descubrir e importar todos los canales de un NVR sin configurar cada cámara individualmente.

---

# 3. Puerto nativo de cámaras IP

## TCP 9008

En muchas cámaras TVT y Provision-ISR, el puerto nativo utilizado por el SDK es:

```text
9008/TCP
```

Este puerto cumple conceptualmente una función similar al 6036 de los NVR.

Ejemplo:

```text
OpenVMS
   |
   | TCP 9008
   v
Provision IP Camera
```

Puede utilizarse para:

- autenticación,
- información del dispositivo,
- configuración,
- streams,
- snapshots,
- eventos,
- PTZ,
- analíticas,
- parámetros de imagen.

> No debe asumirse que **todas** las cámaras Provision-ISR usan 9008. El puerto debe ser detectado o verificado por modelo.

---

# 4. Diferencia entre 6036 y 9008

En términos generales:

```text
NVR / DVR
    |
    +-- Native SDK -> TCP 6036
```

mientras que:

```text
IP Camera
    |
    +-- Native SDK -> TCP 9008
```

Por lo tanto, una integración puede probar inicialmente:

```text
6036 -> NVR / DVR
9008 -> Cámara IP
```

pero siempre debería permitir configurar el puerto manualmente.

---

# 5. RTSP

## Puerto 554

El puerto estándar para video es normalmente:

```text
554/TCP
```

o:

```text
554/UDP
```

RTSP se utiliza para obtener:

- video principal,
- substream,
- audio,
- streams adicionales.

Ejemplo:

```text
OpenVMS
   |
   | RTSP :554
   v
Provision Camera
```

o:

```text
OpenVMS
   |
   | RTSP :554
   v
Provision NVR
   |
   +-- Channel 1
   +-- Channel 2
   +-- Channel 3
```

## Diferencia con el SDK

RTSP transporta principalmente multimedia.

El SDK nativo aporta funciones adicionales:

```text
SDK
 |
 +-- login
 +-- device info
 +-- channels
 +-- status
 +-- recordings
 +-- alarms
 +-- PTZ
 +-- configuration
```

mientras que RTSP:

```text
RTSP
 |
 +-- video
 +-- audio
```

Por eso, para una integración completa, lo ideal es combinar ambos.

---

# 6. HTTP

## Puerto 80

Muchos dispositivos utilizan:

```text
80/TCP
```

para:

- interfaz web,
- API HTTP,
- configuración,
- snapshots,
- eventos,
- ONVIF,
- servicios AJAX / CGI,
- analíticas.

Puede modificarse desde la configuración del dispositivo.

Ejemplo:

```text
http://192.168.1.100/
```

---

# 7. HTTPS

## Puerto 443

Cuando está habilitado:

```text
443/TCP
```

permite administración web cifrada.

Ejemplo:

```text
https://192.168.1.100/
```

En algunos firmwares el HTTPS viene desactivado por defecto.

---

# 8. ONVIF

ONVIF no necesariamente utiliza un puerto exclusivo.

Dependiendo del firmware puede funcionar sobre:

```text
80
443
8899
u otro puerto configurable
```

Por eso no conviene asumir:

```text
ONVIF = puerto fijo
```

La integración debe intentar descubrir el endpoint ONVIF y luego consultar:

```text
/device_service
```

Funciones típicas:

- Device Management,
- Media,
- Media2,
- PTZ,
- Events,
- Imaging,
- Profiles,
- capabilities.

---

# 9. Eventos y analíticas

Los modelos más modernos pueden disponer de:

- LPR,
- reconocimiento de matrículas,
- detección facial,
- clasificación de personas,
- clasificación de vehículos,
- line crossing,
- intrusion,
- detección de objetos,
- metadata AI.

Estas funciones pueden utilizar mecanismos diferentes al SDK principal.

Por ejemplo:

```text
Provision Camera
      |
      +-- Native SDK
      |
      +-- HTTP API
      |
      +-- ONVIF Events
      |
      +-- HTTP POST Events
```

Es importante no asumir que las analíticas pasan obligatoriamente por 6036 o 9008.

---

# 10. API Server

Algunos NVR Provision-ISR modernos incluyen opciones relacionadas con:

```text
API Server
```

Este servicio puede permitir integraciones externas mediante HTTP/API.

No debe confundirse con:

```text
TCP 6036
```

El puerto 6036 corresponde principalmente al protocolo propietario / SDK.

La API HTTP puede utilizar:

```text
HTTP
HTTPS
POST callbacks
REST-like endpoints
CGI
```

dependiendo del firmware.

---

# 11. Arquitectura recomendada para OpenVMS

Para una integración completa se recomienda separar las capas.

```text
                   Provision Device
                         |
       +-----------------+-----------------+
       |                 |                 |
       v                 v                 v
   Native SDK           RTSP          HTTP / ONVIF
       |                 |                 |
   6036 / 9008          554             80 / 443
       |                 |                 |
       +-----------------+-----------------+
                         |
                         v
                      OpenVMS
```

---

# 12. Integración de NVR

Para agregar un NVR:

```text
Manufacturer: Provision-ISR
Device type: NVR
IP: 192.168.1.100
Native Port: 6036
Username: admin
Password: ********
```

OpenVMS podría realizar:

```text
1. TCP connect :6036
2. Login
3. GetDeviceInfo
4. GetCapabilities
5. EnumerateChannels
6. EnumerateStreams
7. GetRTSPUrl
8. Detect PTZ
9. Detect AI capabilities
10. Import channels
```

Resultado:

```text
Provision NVR

CH01 Entrada
CH02 Portón
CH03 Estacionamiento
CH04 Depósito
CH05 Patio
...
```

---

# 13. Integración directa de cámara

Para una cámara:

```text
Manufacturer: Provision-ISR
Device type: IP Camera
IP: 192.168.1.201
Native Port: 9008
RTSP Port: 554
HTTP Port: 80
```

Workflow:

```text
1. probar TCP 9008
2. intentar SDK login
3. obtener device info
4. detectar streams
5. obtener RTSP
6. consultar ONVIF
7. detectar PTZ
8. detectar analíticas
```

---

# 14. Autodetección recomendada

Una integración robusta no debería depender únicamente del puerto.

Puede realizar un fingerprint progresivo.

## Paso 1

Probar puertos:

```text
6036
9008
554
80
443
```

## Paso 2

Si responde 6036:

```text
probable Provision/TVT NVR/DVR
```

## Paso 3

Si responde 9008:

```text
probable Provision/TVT IP Camera
```

## Paso 4

Consultar HTTP:

```text
80 / 443
```

para detectar:

- fabricante,
- modelo,
- firmware,
- API,
- web interface.

## Paso 5

Consultar ONVIF.

## Paso 6

Confirmar mediante SDK:

```text
manufacturer
model
serial
firmware
channel count
```

---

# 15. Puertos sugeridos en configuración

## NVR

```yaml
manufacturer: provision_isr

host: 192.168.1.100

ports:
  native_sdk: 6036
  rtsp: 554
  http: 80
  https: 443
```

## Cámara

```yaml
manufacturer: provision_isr

host: 192.168.1.201

ports:
  native_sdk: 9008
  rtsp: 554
  http: 80
  https: 443
```

---

# 16. Estrategia de fallback

Una buena implementación debería utilizar:

```text
Native SDK
    |
    | falla
    v
ONVIF
    |
    | falla
    v
RTSP
```

Por ejemplo:

```text
Provision Native
      |
      +-- SDK disponible
      |      |
      |      +-- usar capacidades completas
      |
      +-- SDK no disponible
             |
             +-- ONVIF
                    |
                    +-- RTSP
```

---

# 17. Qué protocolo usar para cada función

| Función | Protocolo recomendado |
|---|---|
| Descubrir NVR | SDK 6036 |
| Descubrir cámara | SDK 9008 / ONVIF |
| Enumerar canales NVR | SDK 6036 |
| Live video | RTSP |
| Main / Substream | SDK + RTSP |
| Playback | SDK |
| Buscar grabaciones | SDK |
| Descargar grabaciones | SDK |
| PTZ | SDK u ONVIF |
| Snapshot | SDK / HTTP |
| Estado cámara | SDK |
| LPR | API / SDK / eventos |
| Face Detection | API / SDK / eventos |
| Line Crossing | API / eventos |
| Intrusion | API / eventos |
| Configuración imagen | SDK / ONVIF |
| Device health | SDK |
| Storage | SDK |
| Firmware info | SDK / HTTP |

---

# 18. Consideraciones de seguridad

No se recomienda exponer directamente a Internet:

```text
6036
9008
554
80
```

Es preferible utilizar:

- VPN,
- WireGuard,
- túneles privados,
- redes VLAN,
- firewall,
- ACL.

Ejemplo:

```text
Internet
   |
WireGuard
   |
OpenVMS
   |
VLAN CCTV
   |
Provision NVR
```

---

# 19. Consideraciones para firewalls

Para una instalación típica:

## OpenVMS -> NVR

Permitir:

```text
TCP 6036
TCP 554
TCP 80
TCP 443
```

según servicios utilizados.

## OpenVMS -> Cámaras

Permitir:

```text
TCP 9008
TCP 554
TCP 80
TCP 443
```

cuando corresponda.

No es necesario habilitar todos los puertos si la integración no utiliza todos los servicios.

---

# 20. Recomendación para el plugin Provision-ISR

El plugin debería soportar dos tipos principales:

```text
Provision Device
    |
    +-- NVR / DVR
    |      |
    |      +-- native port: 6036
    |
    +-- IP Camera
           |
           +-- native port: 9008
```

Cada dispositivo debería admitir override manual:

```text
Native SDK Port
RTSP Port
HTTP Port
HTTPS Port
ONVIF Port
```

Nunca deberían quedar hardcodeados como únicos valores posibles.

---

# 21. Conclusión

Para el ecosistema Provision-ISR / TVT, los puertos más importantes para OpenVMS son:

```text
NVR / DVR
---------
6036  Native SDK
554   RTSP
80    HTTP
443   HTTPS


IP Camera
---------
9008  Native SDK
554   RTSP
80    HTTP
443   HTTPS
```

El puerto más relevante para una integración profunda de NVR es:

```text
TCP 6036
```

mientras que en cámaras TVT/Provision suele encontrarse:

```text
TCP 9008
```

RTSP debería quedar reservado principalmente para transporte multimedia, mientras que el SDK nativo debe utilizarse cuando sea posible para discovery, control, playback, eventos, capacidades y administración.

---

## Nota de implementación

Los números anteriores deben considerarse **defaults conocidos**, no valores universales.

El plugin debe:

1. permitir cambiar los puertos,
2. detectar servicios disponibles,
3. identificar capacidades reales,
4. utilizar fallback SDK -> ONVIF -> RTSP,
5. evitar asumir funcionalidades únicamente por el número de puerto.
