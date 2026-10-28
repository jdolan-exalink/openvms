import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useEffect, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { ChannelPicker } from "@/components/servers/ChannelPicker";
import { ProvisionCapabilities } from "@/components/servers/ProvisionCapabilities";
import { Button, ErrorNote, Field, Select, Switch, TextInput } from "@/components/ui";
import { useT } from "@/i18n";

const EMPTY = { site_id: "", name: "", host: "", port: "", https: false, username: "", password: "" };

/**
 * NewProvisionNvr registers a Provision-ISR recorder: it detects the unit first (a read-only login), lets
 * the user pick which channels become cameras and what the recorder can do, then registers it. The password
 * stays in this form; it is sent to the detect and register calls and never shown again.
 */
export function NewProvisionNvr({ sites, onBack, onDone, onCancel, onDirtyChange }: {
  sites: Schemas["Site"][];
  onBack: () => void;
  onDone: () => void;
  onCancel: () => void;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const t = useT();
  const qc = useQueryClient();
  const [form, setForm] = useState(EMPTY);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const dirty = Object.entries(form).some(([k, v]) => v !== EMPTY[k as keyof typeof EMPTY]);
  useEffect(() => {
    onDirtyChange(dirty);
    return () => onDirtyChange(false);
  }, [dirty, onDirtyChange]);

  const connection = () => ({
    site_id: form.site_id,
    host: form.host.trim(),
    port: form.port ? Number(form.port) : undefined,
    https: form.https,
    username: form.username,
    password: form.password,
  });
  const detect = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/servers/provision-nvr/detect", { body: connection() })),
    onSuccess: (det) => setSelected(new Set(det.channels.map((c) => c.id))),
  });
  const create = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/api/v1/servers/provision-nvr", { body: { ...connection(), name: form.name, channel_ids: [...selected] } })),
    onSuccess: async () => {
      await Promise.all(["servers", "sites", "cameras"].map((k) => qc.invalidateQueries({ queryKey: [k] })));
      onDone();
    },
  });
  const det = detect.data;
  const edit = (k: keyof typeof EMPTY) => (e: { target: { value: string } }) => {
    detect.reset();
    setForm({ ...form, [k]: e.target.value });
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!det) detect.mutate();
    else if (selected.size > 0) create.mutate();
  };

  return (
    <form onSubmit={submit} aria-label={t("servers.provisionHeading")} className="flex flex-col gap-4">
      <div>
        <h2 className="text-lg font-bold">{t("servers.provisionHeading")}</h2>
        <p className="mt-1 text-sm text-muted">{t("servers.provisionDescription")}</p>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Sitio">
          <Select required value={form.site_id} onChange={edit("site_id")}>
            <option value="">Elegí un sitio</option>
            {sites.map((s) => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
          </Select>
        </Field>
        <Field label="Nombre">
          <TextInput required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="NVR-Feedlot" />
        </Field>
        <Field label={t("servers.provisionHost")}>
          <TextInput required value={form.host} onChange={edit("host")} placeholder="192.168.1.50" autoComplete="off" />
        </Field>
        <Field label={t("servers.provisionPort")}>
          <TextInput type="number" min={1} max={65535} value={form.port} onChange={edit("port")} placeholder={form.https ? "443" : "80"} />
        </Field>
        <Switch
          className="self-center"
          label={t("servers.provisionHttps")}
          checked={form.https}
          onChange={(next) => {
            detect.reset();
            setForm({ ...form, https: next });
          }}
        />
        <div className="hidden sm:block" />
        <Field label={t("servers.provisionUser")}>
          <TextInput required value={form.username} onChange={edit("username")} autoComplete="off" />
        </Field>
        <Field label={t("servers.provisionPassword")} hint={t("servers.provisionPasswordHint")}>
          <TextInput required type="password" value={form.password} onChange={edit("password")} autoComplete="new-password" />
        </Field>
      </div>

      {det && (
        <section aria-label={t("servers.provisionDetect")} className="flex flex-col gap-4 rounded-m3-lg bg-surface-2 p-4 text-sm">
          <p className="font-semibold">
            {t("servers.provisionDetected", { model: det.device.model, firmware: det.device.firmware, channels: String(det.channels.length) })}
          </p>
          <ChannelPicker channels={det.channels} selected={selected} onChange={setSelected} />
          {selected.size === 0 && <p role="alert" className="text-xs text-warn">{t("servers.provisionChannelsPickAtLeastOne")}</p>}
          <div className="flex flex-col gap-2">
            <h3 className="text-sm font-semibold">{t("servers.provisionCapabilitiesTitle")}</h3>
            <ProvisionCapabilities capabilities={det.capabilities} />
          </div>
        </section>
      )}

      <ErrorNote error={detect.error ?? create.error} />
      <div className="flex flex-wrap gap-2">
        <Button type="submit" variant="primary" disabled={detect.isPending || create.isPending || (!!det && selected.size === 0)}>
          {det
            ? create.isPending ? t("servers.provisionRegistering") : t("servers.provisionRegister")
            : detect.isPending ? t("servers.provisionDetecting") : t("servers.provisionDetect")}
        </Button>
        <Button variant="outlined" onClick={onBack}>Volver</Button>
        <Button variant="text" onClick={onCancel}>Cancelar</Button>
      </div>
    </form>
  );
}
