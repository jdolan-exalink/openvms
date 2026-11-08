import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, type Schemas } from "@/api/client";
import { faceRecurrenceQuery, retentionQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, Card, ErrorNote, Field, PageHeader, Select, Switch, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { fmtBytes } from "@/lib/format";

type Mode = "days" | "forever" | "default";
type Days = { mode: Mode; days: number };

const MAX_DAYS = 3650;

const fromApi = (v: number | null | undefined): Days =>
  v === null || v === undefined ? { mode: "default", days: 30 } : v === 0 ? { mode: "forever", days: 30 } : { mode: "days", days: v };

const toApi = (d: Days): number | null => (d.mode === "default" ? null : d.mode === "forever" ? 0 : d.days);

/** shortens is true when the new period for a class deletes data the current one keeps. */
function shortens(before: number, after: number): boolean {
  if (after === 0) return false;
  return before === 0 || after < before;
}

function DaysInput({ label, value, onChange, allowDefault, disabled }: { label: string; value: Days; onChange: (v: Days) => void; allowDefault?: boolean; disabled?: boolean }) {
  const t = useT();
  return (
    <Field label={label}>
      <div className="flex flex-wrap items-center gap-2">
        <Select aria-label={label} value={value.mode} disabled={disabled} onChange={(e) => onChange({ ...value, mode: e.target.value as Mode })}>
          <option value="days">{t("retention.modeDays")}</option>
          <option value="forever">{t("retention.modeForever")}</option>
          {allowDefault && <option value="default">{t("retention.modeDefault")}</option>}
        </Select>
        {value.mode === "days" && (
          <>
            <TextInput
              type="number"
              aria-label={`${label} · ${t("retention.daysField")}`}
              className="w-28"
              min={1}
              max={MAX_DAYS}
              value={value.days}
              disabled={disabled}
              onChange={(e) => onChange({ ...value, days: Math.min(MAX_DAYS, Math.max(1, Math.trunc(Number(e.target.value)) || 1)) })}
            />
            <span className="text-sm text-on-surface-variant">{t("retention.daysUnit")}</span>
          </>
        )}
      </div>
    </Field>
  );
}

/**
 * The rule that marks a group of unnamed faces as recurrent. It is saved on its own: it changes what the
 * faces page flags, never what is deleted, so it skips the retention confirmation.
 */
function RecurrenceCard() {
  const t = useT();
  const qc = useQueryClient();
  const rule = useQuery(faceRecurrenceQuery);
  const [draft, setDraft] = useState<{ min: number; window: number } | null>(null);
  const [saved, setSaved] = useState(false);
  const save = useMutation({
    mutationFn: async (v: { min: number; window: number }) =>
      unwrap(await api.PUT("/api/v1/system/faces/recurrence", { body: { min_appearances: v.min, window_days: v.window } })),
    onSuccess: () => {
      setDraft(null);
      setSaved(true);
      void qc.invalidateQueries({ queryKey: ["system", "faces", "recurrence"] });
      void qc.invalidateQueries({ queryKey: ["faces"] });
    },
  });
  if (!rule.data) return null;
  const form = draft ?? { min: rule.data.min_appearances, window: rule.data.window_days };
  const edit = (patch: Partial<typeof form>) => {
    setDraft({ ...form, ...patch });
    setSaved(false);
  };
  return (
    <Card title={t("retention.recurrenceTitle")}>
      <p className="text-sm text-on-surface-variant">{t("retention.recurrenceHint")}</p>
      <div className="flex flex-wrap items-end gap-3">
        <Field label={t("retention.recurrenceMin")}>
          <TextInput type="number" className="w-28" min={1} max={100} value={form.min} onChange={(e) => edit({ min: Math.min(100, Math.max(1, Math.trunc(Number(e.target.value)) || 1)) })} />
        </Field>
        <Field label={t("retention.recurrenceWindow")}>
          <TextInput type="number" className="w-28" min={1} max={365} value={form.window} onChange={(e) => edit({ window: Math.min(365, Math.max(1, Math.trunc(Number(e.target.value)) || 1)) })} />
        </Field>
      </div>
      <ErrorNote error={save.error} />
      <div className="flex items-center gap-3">
        <Button variant="tonal" disabled={save.isPending || !draft} onClick={() => save.mutate(form)}>
          {t("retention.recurrenceSave")}
        </Button>
        {saved && <span role="status" className="text-sm text-on-surface-variant">{t("retention.recurrenceSaved")}</span>}
      </div>
    </Card>
  );
}

/**
 * Retention: how long the platform keeps unnamed faces and notifications, and when it cleans up earlier because
 * the data volume is filling. Platform-wide; the API answers 403 to anyone without the platform-level
 * tenant.manage permission.
 */
export function Retention() {
  const t = useT();
  const qc = useQueryClient();
  const view = useQuery(retentionQuery);
  const [draft, setDraft] = useState<{ faces: Days; delivered: Days; read: Days; pressure: boolean; percent: number } | null>(null);
  const [confirm, setConfirm] = useState<Schemas["RetentionStored"] | null>(null);
  const [saved, setSaved] = useState(false);

  const current = view.data?.settings;
  const form =
    draft ??
    (current
      ? {
          faces: fromApi(current.face_unknown_days),
          delivered: fromApi(current.notification_delivery_days),
          read: fromApi(current.notification_read_days),
          pressure: current.pressure_cleanup,
          percent: current.pressure_percent,
        }
      : null);
  const edit = (patch: Partial<NonNullable<typeof form>>) => {
    if (form) setDraft({ ...form, ...patch });
    setSaved(false);
  };

  const save = useMutation({
    mutationFn: async () => {
      if (!form) throw new Error("not loaded");
      return unwrap(
        await api.PUT("/api/v1/system/retention", {
          body: {
            face_unknown_days: toApi(form.faces) ?? 0,
            notification_delivery_days: toApi(form.delivered),
            notification_read_days: toApi(form.read),
            pressure_cleanup: form.pressure,
            pressure_percent: form.percent,
          },
        }),
      );
    },
    onSuccess: () => {
      setDraft(null);
      setConfirm(null);
      setSaved(true);
      void qc.invalidateQueries({ queryKey: ["system", "retention"] });
    },
  });

  const preview = useMutation({
    mutationFn: async (days: number) => unwrap(await api.GET("/api/v1/system/retention/preview", { params: { query: { face_unknown_days: days } } })),
    onSuccess: (stored) => (stored.records > 0 ? setConfirm(stored) : save.mutate()),
  });

  const onSave = () => {
    if (!form || !current) return;
    const next = toApi(form.faces) ?? 0;
    if (shortens(current.face_unknown_days, next)) preview.mutate(next);
    else save.mutate();
  };

  const volume = view.data?.volume;
  const limit = form?.percent ?? 85;
  const pending = view.data?.faces_pending;

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <PageHeader title={t("retention.title")} description={t("retention.description")} />
      <ErrorNote error={view.error} />
      {form && (
        <>
          <Card title={t("retention.facesTitle")}>
            <p className="text-sm text-on-surface-variant">{t("retention.facesHint")}</p>
            <DaysInput label={t("retention.facesTitle")} value={form.faces} onChange={(faces) => edit({ faces })} />
            {pending && <p className="text-sm text-on-surface-variant">{t("retention.facesStored", { count: pending.records, size: fmtBytes(pending.bytes) })}</p>}
          </Card>
          <RecurrenceCard />
          <Card title={t("retention.notificationsTitle")}>
            <DaysInput label={t("retention.deliveredLabel")} value={form.delivered} allowDefault onChange={(delivered) => edit({ delivered })} />
            <DaysInput label={t("retention.readLabel")} value={form.read} allowDefault onChange={(read) => edit({ read })} />
          </Card>
          <Card title={t("retention.volumeTitle")}>
            {volume?.measured ? (
              <div className="flex flex-col gap-2">
                <div className="relative h-3 overflow-hidden rounded-full bg-surface-3" role="img" aria-label={t("retention.volumeUsed", { percent: Math.round(volume.percent), free: fmtBytes(volume.free_bytes), total: fmtBytes(volume.total_bytes) })}>
                  <div className={volume.percent >= limit ? "h-full bg-bad" : "h-full bg-primary"} style={{ width: `${Math.min(100, volume.percent)}%` }} />
                  <div className="absolute top-0 h-full w-0.5 bg-on-surface" style={{ left: `${limit}%` }} />
                </div>
                <p className="text-sm text-on-surface-variant">{t("retention.volumeUsed", { percent: Math.round(volume.percent), free: fmtBytes(volume.free_bytes), total: fmtBytes(volume.total_bytes) })}</p>
              </div>
            ) : (
              <p className="text-sm text-on-surface-variant">{t("retention.volumeUnknown")}</p>
            )}
            <Switch checked={form.pressure} onChange={(pressure) => edit({ pressure })} label={t("retention.pressureLabel")} />
            <p className="text-sm text-on-surface-variant">{t("retention.pressureHint")}</p>
            <Field label={t("retention.pressurePercent")}>
              <TextInput
                type="number"
                className="w-28"
                min={50}
                max={99}
                value={form.percent}
                disabled={!form.pressure}
                onChange={(e) => edit({ percent: Math.min(99, Math.max(50, Math.trunc(Number(e.target.value)) || 50)) })}
              />
            </Field>
          </Card>
          <Card title={t("retention.keptTitle")} variant="outlined">
            <p className="text-sm text-on-surface-variant">{t("retention.keptBody")}</p>
          </Card>
          <ErrorNote error={save.error ?? preview.error} />
          <div className="flex items-center gap-3">
            <Button variant="filled" disabled={save.isPending || preview.isPending || !draft} onClick={onSave}>
              {t("retention.save")}
            </Button>
            {saved && <span role="status" className="text-sm text-on-surface-variant">{t("retention.saved")}</span>}
          </div>
        </>
      )}
      {confirm && (
        <ConfirmDialog
          title={t("retention.confirmTitle")}
          message={t("retention.confirmBody", { days: form?.faces.days ?? 0, count: confirm.records, size: fmtBytes(confirm.bytes) })}
          confirmLabel={t("retention.confirmAction")}
          cancelLabel={t("retention.keep")}
          pending={save.isPending}
          error={save.error}
          onConfirm={() => save.mutate()}
          onCancel={() => setConfirm(null)}
        />
      )}
    </div>
  );
}
