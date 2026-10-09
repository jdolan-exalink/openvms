import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Icon } from "@/components/Icon";
import { type FormEvent, useState } from "react";
import { ApiError, api, type Schemas, unwrap } from "@/api/client";
import { channelsQuery, deliveriesQuery, whatsappQrQuery, whatsappSessionQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { TagInput } from "@/components/TagInput";
import { Button, Checkbox, Empty, ErrorNote, Field, PageHeader, Pill, Select, Summary, Switch, Table, Textarea, TextInput, Th } from "@/components/ui";

type Channel = Schemas["NotificationChannel"];
type ChannelType = Schemas["NotificationChannelType"];
type SecretName = NonNullable<Schemas["NotificationChannelUpdate"]["clear_secrets"]>[number];

const typeLabel: Record<ChannelType, string> = {
  webhook: "Webhook",
  email: "Correo electrónico",
  whatsapp: "WhatsApp",
  telegram: "Telegram",
};
const deliveryLabel: Record<Schemas["NotificationDelivery"]["status"], string> = {
  pending: "Pendiente",
  sent: "Enviada",
  failed: "Fallida",
};

function describeDestinations(c: Channel) {
  const cfg = c.config;
  switch (c.type) {
    case "webhook":
      return cfg.url ?? "";
    case "email":
      return `${cfg.recipients?.length ?? 0} destinatario(s) · ${cfg.host ?? ""}`;
    case "whatsapp":
      return `${cfg.recipients?.length ?? 0} destinatario(s) · sesión ${cfg.session ?? ""}`;
    case "telegram":
      return `${cfg.chat_ids?.length ?? 0} chat(s)`;
  }
}

type TestOutcome = { name: string; results: Schemas["NotificationChannelTestResult"]["results"] };

/** Channels: external notification channels (webhook, email, WhatsApp, Telegram). Requires notifications.manage. */
export function Channels() {
  const t = useT();
  const qc = useQueryClient();
  const channels = useQuery(channelsQuery);
  const deliveries = useQuery(deliveriesQuery());
  const [editing, setEditing] = useState<Channel | "new" | null>(null);
  const [deleting, setDeleting] = useState<Channel | null>(null);
  const [pairing, setPairing] = useState<Channel | null>(null);
  const [outcome, setOutcome] = useState<TestOutcome | null>(null);

  const refresh = () => void qc.invalidateQueries({ queryKey: ["channels"] });
  const toggle = useMutation({
    mutationFn: async (c: Channel) =>
      unwrap(await api.PATCH("/api/v1/notification-channels/{channelId}", { params: { path: { channelId: c.id } }, body: { enabled: !c.enabled } })),
    onSuccess: refresh,
  });
  const remove = useMutation({
    mutationFn: async (c: Channel) =>
      unwrap(await api.DELETE("/api/v1/notification-channels/{channelId}", { params: { path: { channelId: c.id } } })),
    onSuccess: () => {
      setDeleting(null);
      refresh();
      void qc.invalidateQueries({ queryKey: ["rules"] });
    },
  });
  const test = useMutation({
    mutationFn: async (c: Channel): Promise<TestOutcome> => {
      const res = unwrap(await api.POST("/api/v1/notification-channels/{channelId}/test", { params: { path: { channelId: c.id } } }));
      return { name: c.name, results: res.results };
    },
    onSuccess: (o) => {
      setOutcome(o);
      void qc.invalidateQueries({ queryKey: ["deliveries"] });
    },
  });

  if (channels.error instanceof ApiError && channels.error.status === 403) {
    return (
      <div className="mx-auto flex max-w-6xl flex-col gap-6">
        <PageHeader title={t("nav.channels")} />
        <Empty>No tenés permiso para administrar canales. Pedile a un administrador el permiso de gestión de notificaciones.</Empty>
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title={t("nav.channels")}
        description={t("settings.channels")}
        actions={
          <Button variant="filled" onClick={() => setEditing("new")}>
            <Icon icon={Plus} size="xs" /> Nuevo canal
          </Button>
        }
      />
      <ErrorNote error={channels.error ?? toggle.error ?? test.error} />
      {outcome && (
        <div role="status" aria-label="Resultado de la prueba" className="rounded-m3-lg bg-surface-2 px-4 py-3 text-sm">
          <p className="font-medium">Prueba enviada a {outcome.name}</p>
          <ul className="mt-1 flex flex-col gap-1">
            {outcome.results.map((r, i) => (
              <li key={`${r.destination}-${i}`} className={r.ok ? "text-ok" : "text-bad"}>
                {r.destination ? `${r.destination}: ` : ""}
                {r.ok ? "entregada" : `falló (${r.error ?? "error desconocido"})`}
              </li>
            ))}
          </ul>
        </div>
      )}
      {channels.data?.length === 0 && <Empty>No hay canales configurados.</Empty>}
      {!!channels.data?.length && (
        <>
          <Summary>{channels.data.length === 1 ? "1 canal" : `${channels.data.length} canales`}</Summary>
          <ul aria-label="Canales" className="flex flex-col gap-3">
            {channels.data.map((c) => (
              <li key={c.id} className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-m3-lg bg-surface-1 p-4">
                <Switch
                  className="shrink-0 [&>span:last-child]:sr-only"
                  label={`Activar ${c.name}`}
                  checked={c.enabled}
                  disabled={toggle.isPending}
                  onChange={() => toggle.mutate(c)}
                />
                <div className="flex min-w-0 flex-1 basis-56 flex-col gap-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-base font-bold break-words">{c.name}</span>
                    <Pill tone="secondary">{typeLabel[c.type]}</Pill>
                  </div>
                  <div className="truncate font-mono text-xs text-on-surface-variant">{describeDestinations(c)}</div>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {c.type === "whatsapp" && (
                    <Button size="sm" variant="tonal" aria-label={`Vincular ${c.name}`} onClick={() => setPairing(c)}>
                      Vincular
                    </Button>
                  )}
                  <Button size="sm" variant="tonal" aria-label={`Enviar prueba a ${c.name}`} disabled={test.isPending} onClick={() => test.mutate(c)}>
                    Enviar prueba
                  </Button>
                  <Button size="sm" variant="tonal" aria-label={`Editar ${c.name}`} onClick={() => setEditing(c)}>
                    Editar
                  </Button>
                  <Button size="sm" variant="outlined" className="border-bad/60 text-bad" aria-label={`Eliminar ${c.name}`} onClick={() => setDeleting(c)}>
                    Eliminar
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        </>
      )}

      {!!deliveries.data?.length && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-bold">Últimas entregas</h2>
          <Table label="Últimas entregas">
            <thead>
              <tr>
                <Th>Canal</Th>
                <Th>Destino</Th>
                <Th>Estado</Th>
                <Th>Intentos</Th>
                <Th>Detalle</Th>
              </tr>
            </thead>
            <tbody>
              {deliveries.data.map((d) => (
                <tr key={d.id} className="border-t border-outline-variant align-top">
                  <td>{d.channel_name}</td>
                  <td className="font-mono text-xs text-muted">{d.destination || "—"}</td>
                  <td className={d.status === "failed" ? "font-medium text-bad" : d.status === "sent" ? "font-medium text-ok" : ""}>{deliveryLabel[d.status]}</td>
                  <td className="tabular-nums">{d.attempts}</td>
                  <td className="max-w-sm text-xs break-words text-muted">{d.last_error ?? ""}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        </section>
      )}

      {editing && (
        <ChannelForm
          channel={editing === "new" ? undefined : editing}
          onDone={() => {
            setEditing(null);
            refresh();
          }}
          onCancel={() => setEditing(null)}
        />
      )}
      {deleting && (
        <ConfirmDialog
          title="Eliminar canal"
          message={`¿Eliminar el canal ${deleting.name}? Se quitará de las reglas que lo usan.`}
          confirmLabel="Eliminar"
          pending={remove.isPending}
          error={remove.error}
          onConfirm={() => remove.mutate(deleting)}
          onCancel={() => setDeleting(null)}
        />
      )}
      {pairing && <WhatsAppPairing channel={pairing} onClose={() => setPairing(null)} />}
    </div>
  );
}

const defaultPort = { none: 25, starttls: 587, tls: 465 } as const;

function parseHeaders(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    if (!line.trim()) continue;
    const i = line.indexOf(":");
    if (i <= 0) throw new Error("Cabeceras: usá el formato Nombre: valor, una por línea.");
    out[line.slice(0, i).trim()] = line.slice(i + 1).trim();
  }
  return out;
}

function ChannelForm({ channel, onDone, onCancel }: { channel?: Channel; onDone: () => void; onCancel: () => void }) {
  const cfg = channel?.config ?? {};
  const isSet = (name: SecretName) => channel?.secrets_set.includes(name) ?? false;
  const [f, setF] = useState({
    name: channel?.name ?? "",
    type: (channel?.type ?? "webhook") as ChannelType,
    url: cfg.url ?? "",
    host: cfg.host ?? "",
    port: String(cfg.port ?? defaultPort.starttls),
    tls: (cfg.tls ?? "starttls") as NonNullable<Schemas["NotificationChannelConfig"]["tls"]>,
    username: cfg.username ?? "",
    from: cfg.from ?? "",
    recipients: cfg.recipients ?? ([] as string[]),
    session: cfg.session ?? "default",
    chat_ids: cfg.chat_ids ?? ([] as string[]),
    signing_secret: "",
    headers: "",
    smtp_password: "",
    bot_token: "",
    clear: [] as SecretName[],
  });
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((cur) => ({ ...cur, [k]: v }));
  const toggleClear = (name: SecretName) =>
    set("clear", f.clear.includes(name) ? f.clear.filter((n) => n !== name) : [...f.clear, name]);

  const buildConfig = (): Schemas["NotificationChannelConfig"] => {
    switch (f.type) {
      case "webhook":
        return { url: f.url.trim() };
      case "email":
        return {
          host: f.host.trim(), port: Number(f.port) || defaultPort[f.tls], tls: f.tls, from: f.from.trim(),
          recipients: f.recipients, ...(f.username.trim() ? { username: f.username.trim() } : {}),
        };
      case "whatsapp":
        return { session: f.session.trim(), recipients: f.recipients };
      case "telegram":
        return { chat_ids: f.chat_ids };
    }
  };
  const buildSecrets = (): Schemas["NotificationChannelSecrets"] => {
    const s: Schemas["NotificationChannelSecrets"] = {};
    if (f.type === "webhook") {
      if (f.signing_secret) s.signing_secret = f.signing_secret;
      if (f.headers.trim()) s.headers = parseHeaders(f.headers);
    }
    if (f.type === "email" && f.smtp_password) s.smtp_password = f.smtp_password;
    if (f.type === "telegram" && f.bot_token) s.bot_token = f.bot_token;
    return s;
  };

  const save = useMutation({
    mutationFn: async () => {
      const secrets = buildSecrets();
      const hasSecrets = Object.keys(secrets).length > 0;
      if (channel) {
        return unwrap(
          await api.PATCH("/api/v1/notification-channels/{channelId}", {
            params: { path: { channelId: channel.id } },
            body: {
              name: f.name.trim(),
              config: buildConfig(),
              ...(hasSecrets ? { secrets } : {}),
              ...(f.clear.length ? { clear_secrets: f.clear } : {}),
            },
          }),
        );
      }
      return unwrap(
        await api.POST("/api/v1/notification-channels", {
          body: { name: f.name.trim(), type: f.type, enabled: true, config: buildConfig(), ...(hasSecrets ? { secrets } : {}) },
        }),
      );
    },
    onSuccess: onDone,
  });

  const generate = () => {
    const bytes = crypto.getRandomValues(new Uint8Array(24));
    set("signing_secret", Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join(""));
  };

  return (
    <Modal title={channel ? `Editar ${channel.name}` : "Nuevo canal"} onClose={onCancel}>
      <form
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          save.mutate();
        }}
        className="flex flex-col gap-4"
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Nombre">
            <TextInput required maxLength={100} value={f.name} onChange={(e) => set("name", e.target.value)} />
          </Field>
          <Field label="Tipo">
            <Select value={f.type} disabled={!!channel} onChange={(e) => set("type", e.target.value as ChannelType)}>
              {(Object.keys(typeLabel) as ChannelType[]).map((t) => (
                <option key={t} value={t}>
                  {typeLabel[t]}
                </option>
              ))}
            </Select>
          </Field>
        </div>

        {f.type === "webhook" && (
          <>
            <Field label="URL" hint="http o https. Las direcciones link-local y de metadatos (169.254.x.x) no están permitidas.">
              <TextInput required type="url" value={f.url} onChange={(e) => set("url", e.target.value)} />
            </Field>
            <SecretField
              label="Secreto de firma"
              hint="Firma cada envío con HMAC-SHA256 en la cabecera X-OpenVMS-Signature."
              isSet={isSet("signing_secret")}
              cleared={f.clear.includes("signing_secret")}
              onToggleClear={() => toggleClear("signing_secret")}
              clearLabel="Quitar secreto de firma"
            >
              <span className="flex flex-wrap gap-2 sm:flex-nowrap">
                <TextInput type="password" autoComplete="off" value={f.signing_secret} placeholder={isSet("signing_secret") ? "Configurado (dejá vacío para conservarlo)" : ""} onChange={(e) => set("signing_secret", e.target.value)} />
                <Button variant="tonal" onClick={generate}>Generar</Button>
              </span>
            </SecretField>
            <SecretField
              label="Cabeceras personalizadas"
              hint="Una por línea con el formato Nombre: valor. Se guardan cifradas."
              isSet={isSet("headers")}
              cleared={f.clear.includes("headers")}
              onToggleClear={() => toggleClear("headers")}
              clearLabel="Quitar cabeceras"
            >
              <Textarea
                rows={3}
                autoComplete="off"
                value={f.headers}
                placeholder={isSet("headers") ? "Configuradas (escribí para reemplazarlas)" : "Authorization: Bearer …"}
                onChange={(e) => set("headers", e.target.value)}
                className="font-mono"
              />
            </SecretField>
          </>
        )}

        {f.type === "email" && (
          <>
            <div className="grid gap-4 sm:grid-cols-3">
              <Field label="Servidor SMTP">
                <TextInput required value={f.host} onChange={(e) => set("host", e.target.value)} />
              </Field>
              <Field label="Puerto">
                <TextInput required type="number" min={1} max={65535} value={f.port} onChange={(e) => set("port", e.target.value)} />
              </Field>
              <Field label="Cifrado">
                <Select
                  value={f.tls}
                  onChange={(e) => {
                    const tls = e.target.value as typeof f.tls;
                    setF((cur) => ({ ...cur, tls, port: Object.values(defaultPort).map(String).includes(cur.port) ? String(defaultPort[tls]) : cur.port }));
                  }}
                >
                  <option value="starttls">STARTTLS</option>
                  <option value="tls">TLS implícito</option>
                  <option value="none">Sin cifrado</option>
                </Select>
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Usuario SMTP">
                <TextInput autoComplete="off" value={f.username} onChange={(e) => set("username", e.target.value)} />
              </Field>
              <SecretField
                label="Contraseña SMTP"
                isSet={isSet("smtp_password")}
                cleared={f.clear.includes("smtp_password")}
                onToggleClear={() => toggleClear("smtp_password")}
                clearLabel="Quitar contraseña SMTP"
              >
                <TextInput type="password" autoComplete="new-password" value={f.smtp_password} placeholder={isSet("smtp_password") ? "Configurada (dejá vacío para conservarla)" : ""} onChange={(e) => set("smtp_password", e.target.value)} />
              </SecretField>
            </div>
            <Field label="Remitente" hint="Por ejemplo: OpenVMS <vms@ejemplo.com>">
              <TextInput required value={f.from} onChange={(e) => set("from", e.target.value)} />
            </Field>
            <TagInput label="Destinatarios" hint="Direcciones de correo; Enter para agregar cada una." value={f.recipients} onChange={(v) => set("recipients", v)} />
          </>
        )}

        {f.type === "whatsapp" && (
          <>
            <p role="note" className="rounded-m3-lg bg-warn/15 px-4 py-3 text-sm">
              WhatsApp se envía a través de WAHA, que automatiza WhatsApp Web (no oficial): WhatsApp puede bloquear el número.
              Usá un número dedicado, no uno personal ni crítico.
            </p>
            <Field label="Sesión de WAHA" hint="WAHA Core solo admite la sesión «default». Vinculá el teléfono desde el botón Vincular de la lista.">
              <TextInput required value={f.session} onChange={(e) => set("session", e.target.value)} />
            </Field>
            <TagInput
              label="Destinatarios"
              hint="Números con código de país (por ejemplo +54 9 11 5555-5555) o ids de grupo terminados en @g.us."
              value={f.recipients}
              onChange={(v) => set("recipients", v)}
            />
          </>
        )}

        {f.type === "telegram" && (
          <>
            <SecretField
              label="Token del bot"
              hint="Lo entrega @BotFather al crear el bot con /newbot."
              isSet={isSet("bot_token")}
              cleared={false}
              clearLabel=""
            >
              <TextInput
                type="password"
                autoComplete="off"
                required={!channel}
                value={f.bot_token}
                placeholder={isSet("bot_token") ? "Configurado (dejá vacío para conservarlo)" : ""}
                onChange={(e) => set("bot_token", e.target.value)}
              />
            </SecretField>
            <TagInput
              label="Chats"
              hint="Ids numéricos (los grupos y canales son negativos, por ejemplo -1001234567890) o @canal. Agregá el bot al grupo o canal."
              value={f.chat_ids}
              onChange={(v) => set("chat_ids", v)}
            />
          </>
        )}

        <ErrorNote error={save.error} />
        <div className="flex items-center gap-2 pt-2">
          <Button type="submit" variant="filled" disabled={save.isPending}>
            {save.isPending ? "Guardando…" : "Guardar"}
          </Button>
          <Button variant="text" onClick={onCancel}>Cancelar</Button>
        </div>
      </form>
    </Modal>
  );
}

/** A write-only value: never prefilled; shows whether one is stored and lets the user remove it. */
function SecretField({
  label, hint, isSet, cleared, onToggleClear, clearLabel, children,
}: {
  label: string;
  hint?: string;
  isSet: boolean;
  cleared: boolean;
  onToggleClear?: () => void;
  clearLabel: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1">
      <Field label={label} hint={hint}>
        {children}
      </Field>
      {isSet && onToggleClear && (
        <Checkbox checked={cleared} onChange={() => onToggleClear()} label={clearLabel} className="text-xs text-muted" />
      )}
    </div>
  );
}

const sessionLabel: Record<string, string> = {
  NOT_FOUND: "Sin crear",
  STOPPED: "Detenida",
  STARTING: "Iniciando…",
  SCAN_QR_CODE: "Esperando escaneo del código QR",
  WORKING: "Vinculada y funcionando",
  FAILED: "Falló: reiniciá la sesión",
};

/** Pairing panel: proxies WAHA's session status and QR; refreshes while open. */
function WhatsAppPairing({ channel, onClose }: { channel: Channel; onClose: () => void }) {
  const qc = useQueryClient();
  const session = useQuery(whatsappSessionQuery(channel.id));
  const status = session.data?.status;
  const qr = useQuery(whatsappQrQuery(channel.id, status === "SCAN_QR_CODE"));
  const start = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/api/v1/notification-channels/{channelId}/whatsapp/session/start", { params: { path: { channelId: channel.id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["whatsapp-session", channel.id] }),
  });
  const canStart = status === "STOPPED" || status === "NOT_FOUND" || status === "FAILED";

  return (
    <Modal title={`Vincular ${channel.name}`} onClose={onClose}>
      <div className="flex flex-col gap-4">
        <p className="text-sm text-muted">
          Usá un número de WhatsApp dedicado: WAHA usa WhatsApp Web (no oficial) y el número puede ser bloqueado.
        </p>
        <ErrorNote error={session.error ?? start.error} />
        <p role="status" className="text-sm">
          Estado: <strong>{status ? (sessionLabel[status] ?? status) : "consultando…"}</strong>
          {session.data?.phone ? ` (${session.data.phone})` : ""}
        </p>
        {canStart && (
          <div>
            <Button variant="filled" disabled={start.isPending} onClick={() => start.mutate()}>
              Iniciar sesión
            </Button>
          </div>
        )}
        {status === "SCAN_QR_CODE" && (
          <div className="flex flex-col items-center gap-2">
            {qr.data ? (
              <img
                alt="Código QR para vincular WhatsApp"
                src={`data:${qr.data.mimetype};base64,${qr.data.data}`}
                className="size-64 rounded-m3-lg bg-white p-2"
              />
            ) : (
              <p className="text-sm text-muted">Cargando código QR…</p>
            )}
            <p className="text-center text-xs text-muted">
              En el teléfono: WhatsApp &gt; Ajustes &gt; Dispositivos vinculados &gt; Vincular un dispositivo. El código se renueva solo.
            </p>
          </div>
        )}
        <div className="flex justify-end">
          <Button variant="tonal" onClick={onClose}>Cerrar</Button>
        </div>
      </div>
    </Modal>
  );
}
