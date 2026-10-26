# Original Frigate configuration backup

When OpenVMS registers a server it makes the edge agent keep, on the **host**, the Frigate `config.yml` as it
was before OpenVMS changed it. The file lives with the host, not in the OpenVMS database, so it survives
removing the agent, reinstalling OpenVMS and moving the server to another OpenVMS.

## What is stored

Only the Frigate `config.yml` and a few facts about it. Never agent tokens, enrollment tokens or any OpenVMS
credential. Be aware that `config.yml` itself usually contains camera passwords (RTSP URLs) and MQTT
credentials: treat the file as a secret.

Directory `<state dir>/backup/` (`OPENVMS_AGENT_STATE_DIR`, default `/var/lib/openvms-agent`), mode `0700`:

| File | Mode | Content |
|---|---|---|
| `frigate-config.original.yml` | `0600` | the configuration, byte for byte |
| `frigate-config.original.json` | `0600` | `saved_at` (UTC), `frigate_version`, `registered_by` (free text, at most 200 characters, says which OpenVMS registered the server), `sha256` of the YAML, `size` |

The content must be non-empty, valid UTF-8, at most 2 MiB and a YAML mapping.

## First write wins

The first copy the agent receives is the one it keeps. Later writes are refused with `409` and nothing is
replaced, even when several requests arrive at once (the file is published with an atomic hard link). The
only way to store a different original is to delete the backup explicitly (`DELETE`, below).

Which text OpenVMS sends as the original: the `before_yaml` of the oldest stored configuration revision of
the server that recorded one; when OpenVMS never changed that Frigate, the current `config.yml` read from
Frigate at that moment.

## When OpenVMS stores it

Best effort, in the background, never delaying or failing the operation that triggers it (a failure only
logs a warning):

- after connecting an agent by address,
- after a successful agent install over SSH,
- after a successful agent update, so agents deployed before this feature get their copy once they run the
  new version.

It can also be requested from the API: `POST /api/v1/servers/{serverId}/agent/backup` (needs
`servers.manage` and `servers.config.secrets`) and read with `GET` on the same path (needs `servers.manage`;
it returns only the metadata, never the configuration). The first time a copy is stored an
`AGENT_BACKUP_SAVED` audit entry records the server and the sha256, never the configuration. An agent that
predates this feature answers `409 agent_backup_unsupported`: update the agent.

## Agent endpoints

All of them need a token with the **control** scope (`docs/agent/tokens.md`), because the file holds secrets.
The configuration is never logged and never appears in an error answer.

| Request | Answer |
|---|---|
| `GET /v1/backup/original` | `200` `{config, saved_at, frigate_version, registered_by, sha256, size}`; `404` `{"code":"no_backup"}` |
| `PUT /v1/backup/original` `{config, frigate_version, registered_by}` | `201` with the metadata (no config); `409` `{"code":"backup_exists", ...metadata}` when one exists; `400` for an empty, oversize or invalid body |
| `DELETE /v1/backup/original` | `204`; `404` `{"code":"no_backup"}` |

## Moving a server to another OpenVMS

Read the original with `GET /v1/backup/original` (a control token from `openvms-agent connection`), or copy
`frigate-config.original.yml` from the host. The new OpenVMS can compare it with the live configuration and
restore it if the operator wants Frigate back as it was. A plain agent uninstall keeps the backup directory;
`install-agent.sh --uninstall --purge` removes the whole state directory, backup included. Delete it with
`DELETE` or by hand when it is no longer wanted.

## Uninstalling from OpenVMS and restoring the original

The uninstall dialog (Servers, **Uninstall agent**) asks what to do with Frigate's configuration when the
agent holds an original:

- **Keep the current configuration** (default): Frigate is not touched.
- **Restore the original configuration**: before anything is removed, OpenVMS reads the original from the
  agent, writes it to Frigate through the usual validated save and restarts Frigate. Cameras OpenVMS added to
  Frigate may disappear. If the agent has no original, or Frigate rejects the write, the job fails and nothing
  is uninstalled. If the removal fails after a successful restore, the message says the configuration was
  already restored.
- **Keep the backup on the host** (checked by default): leaves `/var/lib/openvms-agent/backup` so another
  OpenVMS can adopt the server. Unchecking it sends `discard_backup: true` and the uninstall also deletes that
  directory.

The same choices exist in the API: `POST /api/v1/servers/{serverId}/agent/uninstall` accepts
`config_action` (`keep` or `restore_original`, default `keep`) and `discard_backup` (default `false`) next to
`ssh_port` and `ssh_password`; an unknown `config_action` is rejected with `400`.

Running `install-agent.sh --uninstall` by hand keeps the backup and prints where it is and how to restore it
(copy it over Frigate's `config.yml` and restart Frigate); `--uninstall --purge` removes it.
