# Edge agent: install and upgrade from GitHub (0.4.0)

One command installs the agent on a Linux host with systemd (amd64 or arm64), with or without OpenVMS
being able to reach the host over SSH. The host does **not** need to reach OpenVMS: OpenVMS is the one
that connects to the agent.

```bash
curl -fsSL https://github.com/jdolan-exalink/openvms/releases/download/agent-v0.4.0/install-agent.sh | sudo bash
```

That URL is the script of release `agent-v0.4.0`; the release page of each version shows its own line.
Options go after `bash -s --`:

```bash
curl -fsSL <release>/install-agent.sh | sudo bash -s -- --port 7419
```

| Option | Meaning |
|---|---|
| `--version X.Y.Z` | install that release instead of the one the script came from |
| `--port N` | port for the first install (default 7419); an existing `agent.env` is never rewritten |
| `--uninstall` | remove the program and the service, keep `/etc/openvms` and `/var/lib/openvms-agent` |
| `--uninstall --purge` | also remove the configuration and the tokens |

## What it does

1. Detects the architecture and downloads `openvms-agent_<version>_linux_<arch>` and `SHA256SUMS` from the
   release. **It refuses to install a binary that does not match the checksum**, and one that does not
   report the expected version when it runs.
2. Installs it in `/usr/local/bin/openvms-agent` (an upgrade keeps the old one as `.prev`).
3. Creates `/etc/openvms/agent.token` the first time (random, `0600`, never printed and never replaced),
   `/etc/openvms/agent.env`, `/var/lib/openvms-agent`, and the `openvms-agent` systemd unit; starts it
   and waits for it to answer.
4. Creates the console token named `openvms` and prints what to enter in OpenVMS:

```text
OpenVMS agent connection
  Agent version  0.4.0
  Address        http://10.0.15.2:7419
                 (use the address OpenVMS reaches: a VPN address, or https://name if you publish it through a tunnel)
  Token          ovmsa_...
Copy the token now; it will not be shown again.
```

Running it again is an **upgrade in place**: same tokens, same configuration. If the new version does not
answer within 30 seconds the script puts the previous binary back and says so; only a healthy upgrade
turns the old binary into `openvms-agent.prev`. An existing `agent.env` is never rewritten: asking for a
different `--port` than the one it sets is refused, and without `--port` the printed address uses the port
the agent really listens on. It tells you how to get a
new `openvms` token (`openvms-agent token rotate openvms`) instead of printing a second one.

## Reaching the agent from OpenVMS

OpenVMS needs an address it can connect to:

- **Same LAN**: the printed `http://<ip>:7419`.
- **VPN** (WireGuard, Tailscale...): the host's VPN address. Recommended when the site also has Frigate:
  video, playback and the Frigate API come along.
- **Zero Trust tunnel** (for example Cloudflare): publish the agent's port as `https://agent.example.com`.
  If the gateway asks for a service token, OpenVMS stores it sealed with the agent.

Publishing the agent is up to you; the agent and its token are the only thing exposed, and the token's
scopes decide what it may do (`docs/agent/tokens.md`). Use `https://` for any address that is not
private: OpenVMS refuses plain HTTP to a public address.

> OpenVMS registering this address and token for a server is the "connect" step of the 0.4.0 work; until
> it lands, the SSH install from the server screen remains the way to register an agent.

## Releasing an agent version (maintainers)

1. `deploy/agent/VERSION` holds the version; `make agent-bump LEVEL=minor NOTE="..."` changes it and adds the
   changelog line. While a version has no tag, further agent changes ride along with it (see
   `docs/VERSIONING.md`).
2. Merge to `main`, then `git tag -a agent-v0.4.0 -m "OpenVMS edge agent 0.4.0" && git push origin agent-v0.4.0`.
3. The workflow **Release agent** checks that the tag equals `deploy/agent/VERSION`, runs the agent and
   installer tests, builds both architectures, and publishes the binaries, `SHA256SUMS` and
   `install-agent.sh` (with the version baked in) on a GitHub release.
4. From then on the version is *released*: the next change to the agent needs a new version
   (`tools/agent_version.sh`, the CI job "Agent version").

The Frigate compose bundle (`deploy/agent/install.sh`) is a different thing and keeps working as before.
