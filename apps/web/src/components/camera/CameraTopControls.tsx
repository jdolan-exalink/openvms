import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Schemas, unwrap } from "@/api/client";
import { ErrorNote, Select, Switch } from "@/components/ui";
import { CAMERA_TYPES, type CameraType, tagsForType } from "@/lib/cameraType";
import { cn } from "@/lib/cn";

type Camera = Schemas["Camera"];

function useCameraPatch(cameraId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: Schemas["CameraUpdate"]) =>
      unwrap(await api.PATCH("/api/v1/cameras/{cameraId}", { params: { path: { cameraId } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cameras"] }),
  });
}

/**
 * One switch for the whole camera: it saves OpenVMS `cameras.enabled` right away and, once that
 * succeeded, stages the Frigate `enabled` section so it goes through the normal review flow.
 */
export function CameraEnabledSwitch({
  camera, canManage, onStageFrigate,
}: {
  camera: Camera;
  canManage: boolean;
  /** Present only when this Frigate version has the `enabled` section and the user may edit it. */
  onStageFrigate?: (enabled: boolean) => void;
}) {
  const patch = useCameraPatch(camera.id);
  const checked = patch.isPending && patch.variables?.enabled !== undefined ? patch.variables.enabled : camera.enabled;
  return (
    <div className="flex flex-col gap-1 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4">
      <Switch
        label="Cámara habilitada"
        checked={checked}
        disabled={!canManage || patch.isPending}
        onChange={(next) => patch.mutate({ enabled: next }, { onSuccess: () => onStageFrigate?.(next) })}
      />
      <p className="text-xs text-muted">
        {onStageFrigate
          ? "Al deshabilitarla también detiene el procesamiento en Frigate: el cambio de Frigate queda pendiente en «Revisar cambios»."
          : "Al deshabilitarla se oculta de las vistas de OpenVMS."}
      </p>
      {!canManage && <p className="text-xs text-muted">Requiere el permiso cameras.manage.</p>}
      <ErrorNote error={patch.error} />
    </div>
  );
}

/** Camera type stored as tags. Choosing LPR · Frigate or leaving it also stages Frigate `lpr.enabled`. */
export function CameraTypeSelect({
  camera, current, canManage, onStageLpr,
}: {
  camera: Camera;
  current: CameraType;
  canManage: boolean;
  /** Present only when this Frigate version has the `lpr` section and the user may edit it. */
  onStageLpr?: (enabled: boolean) => void;
}) {
  const patch = useCameraPatch(camera.id);
  const change = (next: CameraType) => {
    if (next === current) return;
    patch.mutate({ tags: tagsForType(camera.tags, next) }, {
      onSuccess: () => {
        if (next === "lpr_frigate") onStageLpr?.(true);
        else if (next === "generic" && current === "lpr_frigate") onStageLpr?.(false);
      },
    });
  };
  return (
    <div className="flex flex-col gap-2 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4">
      <label className="flex flex-col gap-1 text-sm">
        <span className="font-medium">Tipo de cámara</span>
        <Select aria-label="Tipo de cámara" value={current} disabled={!canManage || patch.isPending} onChange={(e) => change(e.target.value as CameraType)}>
          {CAMERA_TYPES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
        </Select>
      </label>
      <ul className="flex flex-col gap-0.5 text-xs text-muted">
        {CAMERA_TYPES.map((t) => (
          <li key={t.value} className={cn(t.value === current && "font-semibold text-ink")}>
            <span className="font-medium">{t.label}:</span> {t.hint}
          </li>
        ))}
      </ul>
      <ErrorNote error={patch.error} />
    </div>
  );
}
