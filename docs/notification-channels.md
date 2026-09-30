# External notification channels

Rules can notify people outside the web app. A **channel** is a tenant-level, reusable
destination set (webhook, email, WhatsApp, Telegram). Each rule selects which channels to
notify, in addition to its alarm and in-app notification. Everything is managed under the
`notifications.manage` permission (screen "Canales").

## How delivery works

1. A rule fires (event, camera offline, server offline). In the same database transaction the
   engine inserts one `notification_deliveries` row per destination of every enabled selected
   channel (an outbox). Enqueue failures are rolled back to a savepoint and logged: they never
   block rule evaluation.
2. The worker (`apps/worker`) polls the outbox every 5 s, leases due rows
   (`FOR UPDATE SKIP LOCKED`), sends them and records the outcome.
3. Retries: up to 5 attempts with exponential backoff (30 s, 1 m, 2 m, 4 m, capped at 30 m).
   Permanent errors (HTTP 4xx other than 408/429, SMTP 5xx, deleted or disabled channel) fail at
   once. Delivery is at-least-once: a worker crash after sending but before recording can repeat
   a message.
4. Every send is bounded: 10 s dial timeout, 30 s per send. Redirects are not followed and no
   proxy is used.

Delivery history (status `pending`, `sent`, `failed`, attempts, last error) is shown per
channel in the UI and served by `GET /api/v1/notification-deliveries`. History is kept when a
channel or rule is deleted. There is no automatic pruning yet.

## Channel types

| Type | Configuration | Secrets (write-only) |
| --- | --- | --- |
| webhook | `url` (http/https) | `signing_secret`, `headers` |
| email | `host`, `port`, `tls` (`none`, `starttls`, `tls`), `username`, `from`, `recipients` | `smtp_password` |
| whatsapp | `session` (WAHA session), `recipients` (phone numbers or group ids) | none |
| telegram | `chat_ids` | `bot_token` |

Each recipient / chat is a separate delivery, so a retry never repeats a message that already
reached another recipient.

### Webhook payload and signature

`POST` with `Content-Type: application/json`:

```json
{"id":"<delivery id>","type":"openvms.notification","title":"...","body":"...",
 "severity":"info|warning|critical","link":"/alarms","rule_id":"...",
 "occurred_at":"2026-09-29T12:00:00Z","test":false}
```

With a signing secret, two headers are added: `X-OpenVMS-Timestamp` (unix seconds) and
`X-OpenVMS-Signature: sha256=<hex>`, the HMAC-SHA256 of `"<timestamp>.<raw body>"` keyed with the
secret. Receivers should recompute it and reject old timestamps. Custom headers (for example an
`Authorization` token) are stored as secrets.

## Secrets

Secrets are sealed at rest with the same AES-256-GCM mechanism as Frigate passwords
(`OPENVMS_MASTER_KEY`, `internal/secrets`), as one blob per channel bound to the channel id.
The API never returns them: channels expose `secrets_set`, the names of the secrets that hold a
value. On update, omitted secrets keep their value; `clear_secrets` removes them. Audit rows
(`NOTIFICATION_CHANNEL_*`) record names and types, never values.

## Outbound address policy (SSRF)

Webhook URLs must be http or https, without embedded credentials. Connections to **link-local**
addresses (169.254.0.0/16, which includes the cloud metadata endpoint, and fe80::/10),
unspecified and multicast addresses, and the host names `metadata` / `metadata.google.internal`
are refused, both when saving and at connection time (after DNS resolution, so a hostname or a
redirect cannot reach them). The same guard applies to SMTP. **Private LAN and loopback addresses
stay allowed**: on-premise receivers are the normal case for a VMS, so this is not a full SSRF
sandbox. Restrict who holds `notifications.manage` accordingly.

## Retention

The worker prunes history periodically (hourly, in bounded batches of 1000 rows) and logs the
counts as `notification retention pruned`.

| Variable | Default | Effect |
|---|---|---|
| `NOTIFY_DELIVERY_RETENTION` | `720h` (30 days) | Deletes `notification_deliveries` in a terminal state (`sent`, `failed`) created earlier than this. `pending` rows (including in-flight, leased ones) are never deleted. |
| `NOTIFY_READ_RETENTION` | `2160h` (90 days) | Deletes in-app `notifications` that were **read** earlier than this. Unread notifications are kept. |

Values are Go durations (`720h`; there is no `d` unit). A negative value (e.g. `-1h`) disables that
pruning. Deliveries keep `notification_id` as `SET NULL`, so pruning notifications leaves the
delivery history intact.

## WhatsApp (WAHA)

WhatsApp goes through the internal **WAHA** service (`devlikeapro/waha`, the `waha` compose
service). WAHA automates WhatsApp Web, which is **unofficial**: WhatsApp may ban the number.
**Use a dedicated phone number**, not a personal or business-critical one.

`docker-compose.yml` runs `devlikeapro/waha:noweb` (no browser; use `noweb-arm` on ARM), keeps
sessions in the `waha` volume, starts the session `default` on boot, protects the API with
`WAHA_API_KEY`, and publishes no host port. WAHA Core supports only the session name `default`.

Environment (api and worker): `WAHA_BASE_URL` (default `http://waha:3000`; empty disables
WhatsApp channels) and `WAHA_API_KEY`. Set a real `WAHA_API_KEY` outside development.
`WAHA_API_KEY` must match on the `waha`, `api` and `worker` services.

Messages use `POST /api/sendText` with `{session, chatId, text}` and the `X-Api-Key` header.
Recipients are stored as chat ids: a phone number becomes `<digits>@c.us`; group ids end in
`@g.us`. The API proxies pairing (permission `notifications.manage`):
`GET .../whatsapp/session`, `POST .../whatsapp/session/start`, `GET .../whatsapp/qr`.

### Pair the phone

1. `docker compose up -d waha api worker web`.
2. In the UI, open **Canales**, create a WhatsApp channel (session `default`, at least one
   recipient) and open its pairing panel.
3. If the status is `STOPPED` or `NOT_FOUND`, press "Iniciar sesión".
4. When the status is `SCAN_QR_CODE`, on the dedicated phone open WhatsApp, then
   Settings > Linked devices > Link a device, and scan the QR shown in the panel. The QR
   rotates; the panel refreshes it.
5. The status becomes `WORKING`. Press "Enviar prueba".

WAHA API facts (session statuses, `sendText`, QR endpoints, `WHATSAPP_START_SESSION`,
`WHATSAPP_DEFAULT_ENGINE`, the `/app/.sessions` volume) were checked against the WAHA docs
(waha.devlike.pro) through Context7 on 2026-09-29. Not exercised against a live WAHA.

## Telegram

1. In Telegram talk to `@BotFather`, send `/newbot`, choose a name and a username, and copy the
   token it returns.
2. Add the bot to the destination group or channel (as admin for channels), or start a private
   chat with it and send it a message.
3. Find the chat id: open `https://api.telegram.org/bot<TOKEN>/getUpdates` after messaging the
   bot and read `message.chat.id` (groups and channels are negative, for example
   `-1001234567890`). Public channels can use `@channelname`.
4. Create a Telegram channel in "Canales" with the token and the chat ids, then "Enviar prueba".

## Email

Provide the SMTP host, port and mode: `tls` (implicit TLS, usually port 465), `starttls`
(usually 587) or `none` (relays inside a trusted network). SMTP credentials are refused over an
unencrypted connection to a non-loopback host.
