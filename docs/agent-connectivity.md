# Diagnóstico de conectividad del agente

El agente se habla en **dos sentidos**, y un firewall puede dejar pasar solo uno:

| Sentido | Puertos | Para qué |
|---|---|---|
| OpenVMS → agente | 7419 (HTTP) y 7443 (TLS) | estado, operaciones de cámara, web de la cámara, actualización |
| agente → OpenVMS | 8443 (inscripción) y 9443 (gRPC) | inscripción mTLS, heartbeat |

Si OpenVMS alcanza al agente pero el agente no alcanza a OpenVMS, el servidor se ve en línea pero el
agente registra `cannot enroll with the OpenVMS API yet` y no hay heartbeat.

## Qué dice OpenVMS y qué significa

Estos textos salen del estado del agente y del registro (`No se pudo consultar el agente: …`).

| Mensaje | Causa probable | Qué hacer |
|---|---|---|
| `… acepta la conexión pero no responde a tiempo` | el agente está bloqueado o el servidor sobrecargado | en el servidor: `journalctl -u openvms-agent -n 50`, `uptime`, `free -m` |
| `… no responde a nivel de red (sin respuesta en 2 s)` | un firewall descarta el tráfico, ruta o VPN caída, IP equivocada | desde el host de OpenVMS: `nc -vz <ip> 7419`; revisar reglas entre ambas redes |
| `… rechazó la conexión` | el agente no corre o escucha en otro puerto | `systemctl status openvms-agent`; `ss -ltn \| grep 7419` |
| `No hay ruta hasta …` | falta ruta, VPN o gateway | `ip route get <ip>` desde el host de OpenVMS |
| `No se pudo resolver el nombre …` | DNS | registrar el agente con su IP o arreglar el DNS |
| `Falló el TLS …` | el certificado no es el esperado o el puerto no usa TLS | comparar la huella del CA guardado con `openssl x509 -in <cert> -noout -fingerprint -sha256` |

## Comprobaciones rápidas

Desde el host de OpenVMS hacia el agente:

    nc -vz <ip-del-servidor> 7419

En el servidor, hacia OpenVMS (el sentido que más se olvida):

    nc -vz <ip-de-openvms> 8443
    nc -vz <ip-de-openvms> 9443

En el servidor, que el agente responda **en milisegundos** (si tarda, algo lo bloquea):

    curl -s -o /dev/null -w '%{http_code} %{time_total}s\n' \
      -H "Authorization: Bearer $(cat $OPENVMS_AGENT_TOKEN_FILE)" http://127.0.0.1:7419/v1/metrics

Que `/v1/metrics` sin token responda 401 al instante no prueba nada: la petición con token es la que
ejecuta el muestreo.

## Abrir los puertos del agente (Debian y Ubuntu)

`install-agent.sh` lo hace solo cuando `ufw` está activo: abre 7419 y 7443/tcp para `10.0.0.0/8` y
`192.168.0.0/16`, y para `100.64.0.0/10` (Tailscale) si el servidor tiene una dirección de ese rango.
Es idempotente. `--no-firewall` lo omite. Si `ufw` no está activo no toca nada.

A mano, con ufw (el puerto 7419 es el del agente; cambialo si usás otro):

    for net in 10.0.0.0/8 192.168.0.0/16 100.64.0.0/10; do
      for p in 7419 7443; do sudo ufw allow from $net to any port $p proto tcp comment openvms-agent; done
    done

Sin ufw, con nftables (Debian por defecto), si la cadena `input` tiene política `drop`:

    sudo nft add rule inet filter input ip saddr { 10.0.0.0/8, 192.168.0.0/16, 100.64.0.0/10 } tcp dport { 7419, 7443 } accept

Con iptables:

    for net in 10.0.0.0/8 192.168.0.0/16 100.64.0.0/10; do
      sudo iptables -I INPUT -s $net -p tcp -m multiport --dports 7419,7443 -j ACCEPT
    done

Estas reglas solo cubren el sentido OpenVMS → agente. El sentido agente → OpenVMS (8443, 9443) lo decide
el firewall del lado de OpenVMS o el router entre redes.

## Casos conocidos

- **`/v1/metrics` colgado y «agent update not confirmed … rollback»** (agentes anteriores a 0.4.11):
  con el disco por encima del 85 %, cada muestreo recorría todo `/mnt/cctv`, ordenaba millones de
  archivos en memoria y borraba los más viejos, dentro de la petición HTTP. Desde 0.4.11 el muestreo
  solo informa. El borrado es opcional (`OPENVMS_AGENT_STORAGE_PRUNE=true`), corre en segundo plano
  como máximo cada diez minutos, con tope de tiempo y de memoria, y no borra nada si el recorrido no
  terminó. Frigate ya administra su propia retención: activarlo es una decisión del operador.
- **Un solo sentido abierto** (fortunata): OpenVMS (10.1.1.x) alcanza al agente de 192.168.70.x, pero
  el agente no alcanza a OpenVMS. La web de la cámara y las operaciones de cámara funcionan igual;
  faltan la inscripción mTLS y el heartbeat. Hay que abrir 8443 y 9443 del servidor hacia OpenVMS o
  registrar el agente con una dirección de OpenVMS que el servidor sí alcance.
- **Operaciones de cámara que dicen «actualizá el agente»** con el agente al día (anteriores a 0.4.9):
  el listener TLS no montaba las operaciones ONVIF y respondía 404.
