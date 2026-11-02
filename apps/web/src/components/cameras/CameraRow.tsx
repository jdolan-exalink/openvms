import { Settings } from "lucide-react";
import type { Schemas } from "@/api/client";
import { BodyClassifySwitch } from "@/components/BodyClassifySwitch";
import { Icon } from "@/components/Icon";
import { Button, LinkButton, StatusBadge } from "@/components/ui";
import { isVirtualCamera } from "@/lib/cameraTree";

// Column widths shared by the header and the rows; secondary columns drop out as the list narrows (container width).
export const COL = {
  status: "w-28 shrink-0",
  fps: "hidden w-12 shrink-0 text-right @xl:block",
  zones: "hidden w-36 shrink-0 truncate @3xl:block",
  analytics: "w-24 shrink-0 @xl:w-36",
  action: "w-20 shrink-0",
};

export function CameraListHeader({ selectable }: { selectable: boolean }) {
  return (
    <div className="flex items-center gap-3 border-b border-outline-variant bg-surface-2 px-3 py-2 text-xs font-bold text-on-surface-variant">
      {selectable && <span className="size-4 shrink-0"><span className="sr-only">Seleccionar</span></span>}
      <span className="min-w-0 flex-1">Cámara</span>
      <span className={COL.status}>Estado</span>
      <span className={COL.fps}>FPS</span>
      <span className={COL.zones}>Zonas</span>
      <span className={COL.analytics}>Analíticas OpenVMS</span>
      <span className={COL.action}><span className="sr-only">Configuración</span></span>
    </div>
  );
}

/** One camera: name and badges, state, FPS, zones, the analytics switch and the gear (or delete on recorders). */
export function CameraRow({
  camera: c,
  recorder,
  selectable,
  picked,
  onPick,
  analytics,
  canManageCameras,
  canDelete,
  onDelete,
}: {
  camera: Schemas["Camera"];
  recorder: boolean;
  selectable: boolean;
  picked: boolean;
  onPick: () => void;
  /** Policy entry for this camera; undefined means classification is on by default. */
  analytics?: { enabled: boolean; effective?: boolean };
  canManageCameras: boolean;
  canDelete: boolean;
  onDelete: (id: string, name: string) => void;
}) {
  const virtual = isVirtualCamera(c);
  const on = analytics?.enabled ?? true;
  return (
    <div className="flex items-center gap-3 border-b border-outline-variant px-3 py-2 last:border-b-0">
      {selectable && <input type="checkbox" className="size-4 shrink-0" aria-label={`Seleccionar ${c.display_name}`} checked={picked} onChange={onPick} />}
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
          <span className="truncate">{c.display_name}</span>
          {virtual && <span className="rounded-m3-sm bg-surface-3 px-1.5 py-0.5 font-mono text-[10px] text-muted">Virtual</span>}
          {c.lpr && <span className="rounded-m3-sm bg-secondary-container px-1.5 py-0.5 font-mono text-[10px] text-on-secondary-container">LPR</span>}
          {c.onvif_profile ? (
            <span
              className="rounded-m3-sm bg-primary/12 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-primary"
              title={[
                c.onvif_profile,
                c.onvif_version ? `v${c.onvif_version}` : null,
                c.onvif_capabilities?.has_analytics ? "Detecciones/Analítica" : null,
                c.onvif_capabilities?.has_ptz ? "PTZ" : null,
              ].filter(Boolean).join(" · ")}
            >
              {c.onvif_profile}
              {c.onvif_version && <span className="ml-1 opacity-75 font-normal">v{c.onvif_version}</span>}
            </span>
          ) : (
            <span className="rounded-m3-sm border border-outline-variant/60 px-1.5 py-0.5 font-mono text-[10px] text-muted" title="Sin vincular a ONVIF">
              Sin ONVIF
            </span>
          )}
        </div>
        {c.display_name !== c.remote_name && <div className="truncate font-mono text-xs text-muted">{c.remote_name}</div>}
        {c.missing_since && <div role="status" className="text-xs text-warn">{recorder ? "Ya no aparece en el NVR" : "Ya no aparece en Frigate"}</div>}
      </div>
      <div className={COL.status}>{c.enabled ? <StatusBadge status={c.status} /> : <span className="text-xs text-muted">Deshabilitada</span>}</div>
      <div className={`${COL.fps} font-mono text-xs tabular-nums`}>{c.fps != null ? c.fps.toFixed(1) : "—"}</div>
      <div className={`${COL.zones} text-xs text-muted`} title={c.zones.join(", ")}>{c.zones.join(", ") || "—"}</div>
      {/* Only the switch is visible; its label stays for assistive tech. */}
      <div className={`${COL.analytics} [&_button>span:last-child]:sr-only`}>
        {!recorder && !virtual && (
          <BodyClassifySwitch
            scope="camera"
            id={c.id}
            enabled={on}
            disabled={!canManageCameras}
            label={analytics?.effective === false && on ? "Apagada en el servidor" : "Clasificación fina"}
          />
        )}
      </div>
      <div className={`${COL.action} flex justify-end`}>
        {recorder ? (
          canDelete && (
            <Button variant="tonal" size="sm" aria-label={`Eliminar ${c.display_name}`} onClick={() => onDelete(c.id, c.display_name)}>
              Eliminar
            </Button>
          )
        ) : (
          !virtual && (
            <LinkButton
              variant="tonal"
              to="/cameras/$cameraId/frigate"
              params={{ cameraId: c.id }}
              aria-label={`Configuración de ${c.display_name}`}
              title="Configuración"
              className="size-11 shrink-0 px-0"
            >
              <Icon icon={Settings} size="sm" />
            </LinkButton>
          )
        )}
      </div>
    </div>
  );
}
