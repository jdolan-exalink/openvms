# Edge agent: updates over the API, with rollback (0.4.0)

`POST /v1/update` (scope `update`) replaces the agent binary. In 0.4.0 it no longer trusts what it
receives, and an update that does not come up healthy undoes itself.

## What the agent checks before it touches anything

1. The payload is an ELF executable, 64-bit little-endian, **for this machine's architecture**
   (amd64 or arm64), and not over 80 MB. (Central ships one architecture; pushing it to a host of the
   other kind used to leave an agent that could not start.)
2. If OpenVMS sent `X-OpenVMS-SHA256`, the payload matches it. OpenVMS always sends it, so a
   corrupted upload through a tunnel or a proxy is refused.
3. The staged file runs: `openvms-agent.new --version` prints a version, and equals
   `X-OpenVMS-Agent-Version` when OpenVMS sent one.
4. No earlier update is waiting for confirmation (answer 409), so the last known-good binary is never
   overwritten by an unconfirmed one. A pending update whose window and 2 minutes of slack have
   passed is rolled back first.

A refused update changes nothing on disk (no `.new`, no `.prev`, no marker) and answers 400 with the
reason, or 409.

## What happens when it is accepted

1. The running binary is copied to `openvms-agent.prev`, the new one is installed over it, and
   `update-pending.json` is written in the state directory (`/var/lib/openvms-agent`) with a deadline
   of 3 minutes.
2. A systemd timer is armed with `systemd-run --on-active=180s <openvms-agent.prev> rollback-apply`.
   It runs the **previous** binary, so it works even if the new one cannot start at all.
3. The agent answers 202 and restarts.
4. The new process counts its starts in the marker. When it answers its own API (`/v1/metrics`) it
   **confirms**: the marker is deleted and the timer is stopped. `openvms-agent.prev` stays as the last
   known-good binary.

If the new binary never confirms, one of two things puts the previous one back and restarts the
service: the timer (new binary never started, or started and hung) or the new binary itself after more
than 3 failed starts in a row. OpenVMS then sees the old version and reports the agent as not updated.
Without `systemd-run` the update still works but only the crash-loop check protects it; the agent log
says so (`rollback_watchdog=false`).

## From OpenVMS

`UpdateAgent` uploads the bundled binary with the checksum and the version header, through the agent's
stored base URL when it has one (VPN or tunnel; 3 minute upload timeout) or its host and port. It waits
up to 45 seconds for the agent to report the new version. The SSH update (`RunAgentUpdate`) keeps its own
rollback and is unchanged.

## Limits

- The 0.3 to 0.4 step is done by the 0.3 agent, which has none of this. The safety net applies to every
  update **made by a 0.4 agent**.
- The timer needs `systemd-run` and `systemctl` on the host (the agent already requires systemd).
- A binary that runs, answers its API and is wrong in some other way is confirmed. The check proves it
  is alive, not that it is correct.

## By hand

```text
openvms-agent rollback-apply --state-dir /var/lib/openvms-agent --exe /usr/local/bin/openvms-agent
```
Restores the previous binary if an update is pending; does nothing otherwise.
