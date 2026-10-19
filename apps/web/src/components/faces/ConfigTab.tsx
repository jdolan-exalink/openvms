import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { TriangleAlert } from "lucide-react";
import { useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { faceConfigQuery } from "@/api/queries";
import { emptyScope, type Scope, ScopeFilters } from "@/components/faces/ScopeFilters";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button, Card, Empty, ErrorNote, Field, Select, StatusBadge, Switch, TextInput } from "@/components/ui";
import { useT, type MessageKey } from "@/i18n";
import { loadRestart, saveRestart } from "@/lib/frigateRestart";

type ServerConfig = Schemas["FaceServerConfig"];
type Patch = Schemas["FaceConfigPatch"];

/** Number fields of the global section with Frigate's documented ranges (mirrored by the API). */
const numberFields = [
  { key: "detection_threshold", label: "faces.cfgDetection", min: 0, max: 1, step: 0.01, int: false },
  { key: "recognition_threshold", label: "faces.cfgRecognition", min: 0, max: 1, step: 0.01, int: false },
  { key: "unknown_score", label: "faces.cfgUnknown", min: 0, max: 1, step: 0.01, int: false },
  { key: "min_faces", label: "faces.cfgMinFaces", min: 1, max: 6, step: 1, int: true },
  { key: "min_area", label: "faces.cfgMinArea", min: 0, max: 1_000_000, step: 1, int: true },
  { key: "save_attempts", label: "faces.cfgSaveAttempts", min: 0, max: 10_000, step: 1, int: true },
] as const satisfies readonly { key: keyof Patch; label: MessageKey; min: number; max: number; step: number; int: boolean }[];

type NumberKey = (typeof numberFields)[number]["key"];

/** ConfigTab lists the face recognition settings of every server and edits them (faces.manage + servers.config). */
export function ConfigTab({ canRestart }: { canRestart: boolean }) {
  const t = useT();
  const [scope, setScope] = useState<Scope>(emptyScope);
  const config = useQuery(faceConfigQuery({ site_id: scope.site ? [scope.site] : undefined }));
  const shown = config.data?.filter((s) => !scope.server || s.server_id === scope.server);
  const [editing, setEditing] = useState<string | null>(null);

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-on-surface-variant">{t("faces.cfgIntro")}</p>
      <ScopeFilters value={scope} onChange={setScope} />
      <ErrorNote error={config.error} />
      {shown && shown.length === 0 && <Empty>{t("faces.cfgEmpty")}</Empty>}
      {shown && shown.length > 0 && (
        <ul className="flex flex-col gap-3" aria-label={t("faces.cfgList")}>
          {shown.map((s) => (
            <li key={s.server_id}>
              <ServerCard server={s} open={editing === s.server_id} onToggle={() => setEditing(editing === s.server_id ? null : s.server_id)} canRestart={canRestart} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ServerCard({ server, open, onToggle, canRestart }: { server: ServerConfig; open: boolean; onToggle: () => void; canRestart: boolean }) {
  const t = useT();
  const [restart, setRestartState] = useState<"pending" | "done" | null>(() => (loadRestart(server.server_id) ? "pending" : null));
  const setRestart = (next: "pending" | "done" | null) => {
    saveRestart(server.server_id, next === "pending");
    setRestartState(next);
  };
  const readable = server.enabled !== undefined;
  const camerasOn = server.cameras.filter((c) => c.enabled).length;

  return (
    <Card>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col gap-0.5">
          <span className="font-medium">{server.server_name}</span>
          <span className="text-xs text-muted">{server.site_name} · Frigate {server.frigate_version}</span>
        </div>
        <div className="flex items-center gap-3">
          {readable && <StatusBadge status={server.enabled ? "on" : "off"} tone={server.enabled ? "ok" : "neutral"} label={server.enabled ? t("faces.cfgOn") : t("faces.cfgOff")} />}
          {server.can_edit && readable && (
            <Button variant="tonal" aria-label={t("faces.cfgEdit", { name: server.server_name })} onClick={onToggle}>
              {open ? t("faces.cfgClose") : t("faces.cfgEdit", { name: server.server_name })}
            </Button>
          )}
        </div>
      </div>
      {server.error && <p role="alert" className="text-sm text-bad">{t("faces.cfgUnreadable", { error: server.error })}</p>}
      {readable && (
        <p className="text-sm text-on-surface-variant">
          {t("faces.cfgSummary", { model: server.model_size ?? "", rec: String(server.recognition_threshold ?? ""), on: camerasOn, total: server.cameras.length })}
        </p>
      )}
      {!server.can_edit && readable && <p className="text-xs text-muted">{t("faces.cfgReadOnly")}</p>}
      {restart === "pending" && <RestartNotice serverId={server.server_id} canRestart={canRestart} onDone={() => setRestart("done")} />}
      {restart === "done" && <p role="status" className="text-sm text-ok">{t("faces.cfgRestartDone")}</p>}
      {open && server.can_edit && readable && <Editor server={server} onSaved={() => setRestart("pending")} />}
    </Card>
  );
}

function RestartNotice({ serverId, canRestart, onDone }: { serverId: string; canRestart: boolean; onDone: () => void }) {
  const t = useT();
  const [confirming, setConfirming] = useState(false);
  const restart = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/servers/{serverId}/restart", { params: { path: { serverId } } })),
    onSuccess: () => {
      setConfirming(false);
      onDone();
    },
  });
  return (
    <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded-m3-lg bg-warn/10 px-4 py-3 text-sm">
      <span className="flex items-center gap-2 text-warn">
        <TriangleAlert className="size-4" aria-hidden /> {t("faces.cfgRestartNotice")}
      </span>
      {canRestart ? (
        <Button size="sm" onClick={() => setConfirming(true)}>{t("faces.cfgRestart")}</Button>
      ) : (
        <span className="text-xs text-muted">{t("faces.cfgNoRestartPerm")}</span>
      )}
      {confirming && (
        <ConfirmDialog
          title={t("faces.cfgRestartTitle")}
          message={t("faces.cfgRestartMessage")}
          confirmLabel={t("faces.cfgRestart")}
          pending={restart.isPending}
          error={restart.error}
          onConfirm={() => restart.mutate()}
          onCancel={() => setConfirming(false)}
        />
      )}
    </div>
  );
}

function Editor({ server, onSaved }: { server: ServerConfig; onSaved: () => void }) {
  const t = useT();
  const qc = useQueryClient();
  const [enabled, setEnabled] = useState(server.enabled ?? false);
  const [model, setModel] = useState<string>(server.model_size ?? "small");
  const [blur, setBlur] = useState(server.blur_confidence_filter ?? true);
  const [nums, setNums] = useState<Record<NumberKey, string>>(() =>
    Object.fromEntries(numberFields.map((f) => [f.key, String(server[f.key] ?? "")])) as Record<NumberKey, string>,
  );
  const [camEnabled, setCamEnabled] = useState<Record<string, boolean>>({});
  const [camArea, setCamArea] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<string[]>([]);
  const [noChanges, setNoChanges] = useState(false);

  const save = useMutation({
    mutationFn: async (body: Patch) => unwrap(await api.PATCH("/api/v1/faces/config/{serverId}", { params: { path: { serverId: server.server_id } }, body })),
    onSuccess: async () => {
      onSaved();
      await qc.invalidateQueries({ queryKey: ["faces", "config"] });
    },
  });

  function submit() {
    const problems: string[] = [];
    const body: Patch = {};
    if (enabled !== server.enabled) body.enabled = enabled;
    if (model !== server.model_size) body.model_size = model as Patch["model_size"];
    if (blur !== server.blur_confidence_filter) body.blur_confidence_filter = blur;
    const check = (label: MessageKey, raw: string, min: number, max: number, int: boolean): number | undefined => {
      const n = Number(raw);
      if (raw.trim() === "" || !Number.isFinite(n) || n < min || n > max || (int && !Number.isInteger(n))) {
        problems.push(t("faces.cfgRange", { field: t(label), min, max }));
        return undefined;
      }
      return n;
    };
    for (const f of numberFields) {
      if (nums[f.key] === String(server[f.key] ?? "")) continue;
      const n = check(f.label, nums[f.key], f.min, f.max, f.int);
      if (n !== undefined) (body as Record<string, unknown>)[f.key] = n;
    }
    const cameras: NonNullable<Patch["cameras"]> = [];
    for (const cam of server.cameras) {
      const entry: NonNullable<Patch["cameras"]>[number] = { camera_id: cam.camera_id };
      if (cam.camera_id in camEnabled && camEnabled[cam.camera_id] !== cam.enabled) entry.enabled = camEnabled[cam.camera_id];
      const raw = camArea[cam.camera_id];
      if (raw !== undefined && raw !== String(cam.min_area ?? "")) {
        const n = check("faces.cfgCameraMinArea", raw, 0, 1_000_000, true);
        if (n !== undefined) entry.min_area = n;
      }
      if (entry.enabled !== undefined || entry.min_area !== undefined) cameras.push(entry);
    }
    if (cameras.length > 0) body.cameras = cameras;
    setErrors(problems);
    setNoChanges(false);
    if (problems.length > 0) return;
    if (Object.keys(body).length === 0) {
      setNoChanges(true);
      return;
    }
    save.mutate(body);
  }

  return (
    <form noValidate className="flex flex-col gap-4 rounded-m3-xl bg-surface-1 p-4" onSubmit={(e) => { e.preventDefault(); submit(); }}>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <Switch checked={enabled} onChange={setEnabled} label={t("faces.cfgEnabled")} />
        <Field label={t("faces.cfgModel")}>
          <Select value={model} onChange={(e) => setModel(e.target.value)}>
            <option value="small">{t("faces.cfgModelSmall")}</option>
            <option value="large">{t("faces.cfgModelLarge")}</option>
          </Select>
        </Field>
        <Switch checked={blur} onChange={setBlur} label={t("faces.cfgBlur")} />
        {numberFields.map((f) => (
          <Field key={f.key} label={t(f.label)}>
            <TextInput type="number" inputMode="decimal" min={f.min} max={f.max} step={f.step} value={nums[f.key]} onChange={(e) => setNums({ ...nums, [f.key]: e.target.value })} />
          </Field>
        ))}
      </div>
      {server.cameras.length > 0 && (
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-1 text-sm font-medium">{t("faces.cfgCameras")}</legend>
          {server.cameras.map((cam) => (
            <div key={cam.camera_id} className="flex flex-wrap items-center justify-between gap-3">
              <Switch
                checked={camEnabled[cam.camera_id] ?? cam.enabled}
                onChange={(on) => setCamEnabled({ ...camEnabled, [cam.camera_id]: on })}
                label={t("faces.cfgCameraEnabled", { name: cam.name })}
              />
              <TextInput
                type="number"
                className="w-36"
                min={0}
                max={1_000_000}
                aria-label={t("faces.cfgCameraMinArea", { name: cam.name })}
                value={camArea[cam.camera_id] ?? String(cam.min_area ?? "")}
                onChange={(e) => setCamArea({ ...camArea, [cam.camera_id]: e.target.value })}
              />
            </div>
          ))}
        </fieldset>
      )}
      {errors.length > 0 && (
        <ul role="alert" className="text-sm text-bad">
          {errors.map((e) => <li key={e}>{e}</li>)}
        </ul>
      )}
      {noChanges && <p role="status" className="text-sm text-muted">{t("faces.cfgNoChanges")}</p>}
      <ErrorNote error={save.error} />
      <div>
        <Button type="submit" variant="filled" disabled={save.isPending}>
          {save.isPending ? t("faces.saving") : t("faces.cfgSave")}
        </Button>
      </div>
    </form>
  );
}
