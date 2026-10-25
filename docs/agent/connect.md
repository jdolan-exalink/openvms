# Connecting an agent to OpenVMS (0.4.0)

OpenVMS always starts the connection: it calls the agent's REST API. The host of the agent does not need
to reach OpenVMS, which is what makes a server behind a NAT, on another network or on a VPN workable.

## Steps

1. Install the agent on the host: `docs/agent/install.md` (one `curl` command). It prints the address and
   a token. To get them again, or to make a token with fewer rights: `docs/agent/tokens.md`.
2. In OpenVMS, **Servidores** > the server's row > **Conectar agente existente**:
   - **Dirección del agente**: where OpenVMS reaches it. `http://10.8.0.5:7419` on a LAN or VPN;
     `https://agent.example.com` if you publish it through a tunnel.
   - **Token del agente**: the one printed on the host. It is stored sealed and never shown again.
   - **Opciones avanzadas**: headers a gateway in front of the agent asks for (a Cloudflare Access service
     token: `CF-Access-Client-Id` and `CF-Access-Client-Secret`), stored sealed; and the certificate
     authority of an agent with a private https certificate.
3. OpenVMS calls the agent with the token **before storing anything**. If it answers, the agent is
   registered and the screen shows its version and what the token allows (reading the agent, collecting
   plate readings, camera control). If not, nothing changes and the message says why: unreachable, token
   refused, token without the read scope, or not an OpenVMS agent.

From the console of the OpenVMS host:

```text
vmsctl agent-connect -server-id UUID -session-file FILE -url https://agent.example.com \
    -token-file TOKEN [-headers-file headers.json] [-ca-file ca.pem] [-replace]
```
The token, the headers and the CA come from files (private ones for the token and the headers) so no secret
appears in a command line. The API is `PUT/GET/DELETE /api/v1/servers/{id}/agent/connection`.

Needs `servers.manage` and `servers.config.secrets` on the server. A server that already has an agent asks for
an explicit confirmation to replace it (`replace_existing`, the checkbox, `-replace`); a failed attempt never
replaces a working registration. **Desconectar** forgets an agent connected this way; an agent installed
over SSH is not affected (it has its own uninstall).

## What OpenVMS refuses

OpenVMS will not connect to this host (loopback), link-local addresses (where cloud metadata lives), multicast or
unspecified addresses, whatever name or address is typed; it resolves a name once and connects to the address it
checked. Plain `http://` is accepted only for private addresses (10/8, 172.16/12, 192.168/16, 100.64/10 used by
VPN overlays, private IPv6); a public address needs `https://`.

## Topologies

| Situation | Address to register | Notes |
|---|---|---|
| Same LAN | `http://<lan ip>:7419` | the SSH install from the same screen also works |
| VPN (WireGuard, Tailscale...) | `http://<vpn ip>:7419` | recommended when the site also has Frigate: video, playback and the Frigate API travel on the same private network. Let the agent listen only on the VPN interface (`OPENVMS_AGENT_LISTEN`). |
| Zero Trust tunnel | `https://agent.example.com` | publish only the agent's API; add the gateway's service token under advanced options. Not suited to Frigate video. |
| Frigate and agent at different addresses | each one in its own place | the Frigate URL is the server's own field; the agent's address is independent of it |

## After connecting

OpenVMS asks the agent for its state every 15 seconds (no heartbeat from the agent is needed), collects its plate
readings (`docs/provision-isr/ingestion.md`) and updates it through the same address (`docs/agent/update.md`).
