# Testing the agent installer on a real host

Spanish version: [real-host-test.es.md](real-host-test.es.md).

A checklist for the first run of agent 0.4.0 on a real machine (a Frigate host, ideally behind NAT).
Nothing here has been run on a real host yet: record what you see, especially where it differs.

You need: root on a Linux host with systemd (amd64 or arm64), outbound HTTPS to GitHub, and an OpenVMS
that can reach the host (LAN, VPN or a tunnel). The host does **not** need to reach OpenVMS.

## 1. Install

```bash
curl -fsSL https://github.com/jdolan-exalink/openvms/releases/download/agent-v0.4.0/install-agent.sh | sudo bash
```

Expect: the architecture detected, the checksum verified, the service started, and a block
"OpenVMS agent connection" with the version, an address and a token `ovmsa_...`. Copy the token.

Check on the host:

```bash
systemctl is-active openvms-agent          # active
/usr/local/bin/openvms-agent --version     # 0.4.0
ls -l /etc/openvms /var/lib/openvms-agent  # agent.token and the tokens file are 0600
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7419/v1/metrics   # 401 (no token)
```

## 2. Connect from OpenVMS

Use the address OpenVMS reaches (not necessarily the one printed), then in the server row choose
"Conectar agente", or from the console:

```bash
vmsctl agent-connect --help
```

Expect: OpenVMS answers with what the token allows (read, events, control, update). A wrong token or an
unreachable address must be refused with a clear message and store nothing.

## 3. Behind NAT or without a route back

Install an agent on a host that OpenVMS reaches but that cannot reach OpenVMS. Expect:

- The install does not fail with `enrollment_verification`; it ends in pull mode.
- The server shows the agent as online within about a minute (OpenVMS polls `/v1/metrics`).

## 4. Idempotent re-run

Run the same command again. Expect: an upgrade in place, no new token printed, the same address,
`agent.env` unchanged. `--port` different from the existing one is refused.

## 5. Update and rollback

Ask for a bad update and for a good one:

1. Update from OpenVMS to the bundled binary (server row, update). Expect the version to change and the
   agent to come back online; `/usr/local/bin/openvms-agent.prev` holds the old binary.
2. To see the rollback, stop confirming the new binary: with an update pending, make the new service
   fail to answer (for example `systemctl stop openvms-agent` right after the update starts). Expect the
   previous binary back within a few minutes, and the agent online again.
   If you can do this only once, do step 1.

Manual check of the safety net: `openvms-agent rollback-apply --state-dir /var/lib/openvms-agent --exe /usr/local/bin/openvms-agent`
does nothing when no update is pending.

## 6. Tokens

```bash
sudo openvms-agent token list
sudo openvms-agent token create --help
sudo openvms-agent token rotate openvms
```

Expect: after rotating, OpenVMS gets a 401 until you update the token in "Conectar agente".

## 7. Uninstall (optional)

```bash
curl -fsSL <release>/install-agent.sh | sudo bash -s -- --uninstall
```

The program and the service go; `/etc/openvms` and the state stay. `--purge` removes them too.

## What to report

For each step: what happened, the exact message if it failed, and `journalctl -u openvms-agent -n 50`.
Steps 3 and 5 are the ones the automated tests could not cover.
