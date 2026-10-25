# Probar el instalador del agente en un host real

Lista de verificación para la primera ejecución del agente 0.4.0 en una máquina real (idealmente un host
de Frigate detrás de NAT). Versión en inglés: [real-host-test.md](real-host-test.md).
Nada de esto se probó todavía en un host real: anotá lo que veas, sobre todo donde difiera.

Necesitás: root en un Linux con systemd (amd64 o arm64), salida HTTPS a GitHub y un OpenVMS que pueda
llegar al host (LAN, VPN o túnel). El host **no** necesita llegar a OpenVMS.

## 1. Instalar

```bash
curl -fsSL https://github.com/jdolan-exalink/openvms/releases/download/agent-v0.4.0/install-agent.sh | sudo bash
```

Esperado: arquitectura detectada, checksum verificado, servicio iniciado y un bloque
"OpenVMS agent connection" con la versión, una dirección y un token `ovmsa_...`. Copiá el token.

Verificar en el host:

```bash
systemctl is-active openvms-agent          # active
/usr/local/bin/openvms-agent --version     # 0.4.0
ls -l /etc/openvms /var/lib/openvms-agent  # agent.token y el archivo de tokens son 0600
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7419/v1/metrics   # 401 (sin token)
```

## 2. Conectar desde OpenVMS

Usá la dirección a la que OpenVMS llega (no necesariamente la impresa). En la fila del servidor elegí
"Conectar agente", o desde la consola:

```bash
vmsctl agent-connect --help
```

Esperado: OpenVMS responde qué permite el token (read, events, control, update). Un token incorrecto o una
dirección inalcanzable deben rechazarse con un mensaje claro y sin guardar nada.

## 3. Detrás de NAT o sin ruta de vuelta

Instalá un agente en un host al que OpenVMS llega pero que no puede llegar a OpenVMS. Esperado:

- La instalación no falla con `enrollment_verification`; termina en modo pull.
- El servidor muestra el agente online en aproximadamente un minuto (OpenVMS consulta `/v1/metrics`).

## 4. Re-ejecución idempotente

Corré el mismo comando otra vez. Esperado: upgrade en el lugar, sin token nuevo, la misma dirección y
`agent.env` sin cambios. Un `--port` distinto del existente se rechaza.

## 5. Actualización y rollback

1. Actualizá desde OpenVMS al binario incluido (fila del servidor, actualizar). Esperado: la versión cambia,
   el agente vuelve online y `/usr/local/bin/openvms-agent.prev` guarda el binario anterior.
2. Para ver el rollback, impedí que el binario nuevo confirme: con una actualización pendiente, hacé que el
   servicio nuevo no responda (por ejemplo `systemctl stop openvms-agent` justo después de iniciar la
   actualización). Esperado: vuelve el binario anterior en pocos minutos y el agente queda online.
   Esto último no está verificado que dispare el rollback; si solo podés hacer una prueba, hacé el punto 1.

Chequeo manual de la red de seguridad:
`openvms-agent rollback-apply --state-dir /var/lib/openvms-agent --exe /usr/local/bin/openvms-agent`
no hace nada si no hay una actualización pendiente.

## 6. Tokens

```bash
sudo openvms-agent token list
sudo openvms-agent token create --help
sudo openvms-agent token rotate openvms
```

Esperado: tras rotar, OpenVMS recibe 401 hasta que actualices el token en "Conectar agente".

## 7. Desinstalar (opcional)

```bash
curl -fsSL <release>/install-agent.sh | sudo bash -s -- --uninstall
```

Se van el programa y el servicio; `/etc/openvms` y el estado quedan. `--purge` los borra también.

## Qué reportar

Por cada paso: qué pasó, el mensaje exacto si falló y `journalctl -u openvms-agent -n 50`.
Los pasos 3 y 5 son los que los tests automáticos no pudieron cubrir.
