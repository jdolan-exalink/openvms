import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { Schemas } from "@/api/client";
import { cameraPtzPresetsQuery } from "@/api/queries";
import { Button, ErrorNote, TextInput } from "@/components/ui";

export const LIMITS_UNAVAILABLE = "Los límites de movimiento no se exponen por ONVIF PTZ en esta versión.";
export const AGENT_OUTDATED = "Actualizá el agente para editar presets y la posición home.";
export const CAPABILITIES_UNKNOWN = "No se pudieron leer las capacidades PTZ de la cámara.";
export const NO_PRESET_STORAGE = "La cámara no guarda presets.";
export const NO_HOME = "La cámara no tiene posición home.";
export const HOME_FIXED = "La posición home de esta cámara es fija y no se puede guardar.";
export const NO_PERMISSION = "Se necesita el permiso de PTZ para editar presets y la posición home.";

type Caps = Schemas["CameraPtzCapabilities"];

/** Why a control is disabled (undefined: it is enabled), from what the device declared. */
export function editReasons(caps: Caps | undefined, canManage: boolean) {
  if (!canManage) return { presets: NO_PERMISSION, goHome: NO_PERMISSION, setHome: NO_PERMISSION };
  if (!caps) return { presets: CAPABILITIES_UNKNOWN, goHome: CAPABILITIES_UNKNOWN, setHome: CAPABILITIES_UNKNOWN };
  if (!caps.known) {
    const why = caps.reason === "agent_outdated" ? AGENT_OUTDATED : CAPABILITIES_UNKNOWN;
    return { presets: why, goHome: why, setHome: why };
  }
  return {
    presets: caps.preset_edit ? undefined : NO_PRESET_STORAGE,
    goHome: caps.home ? undefined : NO_HOME,
    setHome: !caps.home ? NO_HOME : caps.home_set ? undefined : HOME_FIXED,
  };
}

type Props = {
  cameraId: string;
  canManage: boolean;
  busy: boolean;
  onGo: (token: string) => void;
  onSave: (name: string, token?: string) => void;
  onRemove: (token: string) => void;
  onGoHome: () => void;
  onSetHome: () => void;
};

/** Presets reported by the camera: run, create, rename and delete them, and the home position, as far as the device supports. */
export function PresetsPanel({ cameraId, canManage, busy, onGo, onSave, onRemove, onGoHome, onSetHome }: Props) {
  const presets = useQuery({ ...cameraPtzPresetsQuery(cameraId), enabled: canManage });
  const list = presets.data?.presets ?? [];
  const reasons = editReasons(presets.data?.capabilities, canManage);
  const [name, setName] = useState("");
  const [renaming, setRenaming] = useState<{ token: string; name: string } | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const shown = [reasons.presets, reasons.goHome, reasons.setHome].filter((r, i, all) => r && all.indexOf(r) === i);
  return (
    <section aria-label="Presets" className="flex flex-col gap-3 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-4">
      <h3 className="text-sm font-semibold">Presets</h3>
      {!canManage && <p className="text-xs text-muted">Se necesita el permiso de PTZ para ver y ejecutar presets.</p>}
      {canManage && presets.isLoading && <p className="text-xs text-muted">Leyendo presets de la cámara…</p>}
      {canManage && presets.isError && <ErrorNote error={presets.error} />}
      {canManage && presets.isSuccess && list.length === 0 && <p className="text-xs text-muted">La cámara no tiene presets guardados.</p>}
      <ul className="flex flex-col gap-1.5">
        {list.map((p) => (
          <li key={p.token} className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span className="truncate">{p.name || `Preset ${p.token}`} <span className="font-mono text-[10px] text-muted">#{p.token}</span></span>
            <span className="flex flex-wrap gap-1.5">
              <Button size="sm" disabled={busy || !p.token} onClick={() => p.token && onGo(p.token)} aria-label={`Ir a ${p.name || p.token}`}>Ir</Button>
              <Button size="sm" disabled={busy || !!reasons.presets || !p.token} title={reasons.presets} onClick={() => p.token && setRenaming({ token: p.token, name: p.name ?? "" })} aria-label={`Renombrar ${p.name || p.token}`}>Renombrar</Button>
              <Button size="sm" disabled={busy || !!reasons.presets || !p.token} title={reasons.presets} onClick={() => p.token && setRemoving(p.token)} aria-label={`Eliminar ${p.name || p.token}`}>Eliminar</Button>
            </span>
            {renaming && renaming.token === p.token && (
              <RenameForm
                initial={renaming.name}
                busy={busy}
                onSubmit={(next) => {
                  onSave(next, p.token);
                  setRenaming(null);
                }}
                onCancel={() => setRenaming(null)}
              />
            )}
            {removing === p.token && (
              <span role="group" aria-label="Confirmar eliminación" className="flex w-full items-center gap-2 text-xs">
                ¿Eliminar este preset de la cámara?
                <Button size="sm" variant="danger" disabled={busy} onClick={() => { onRemove(p.token!); setRemoving(null); }}>Eliminar definitivamente</Button>
                <Button size="sm" onClick={() => setRemoving(null)}>Cancelar</Button>
              </span>
            )}
          </li>
        ))}
      </ul>
      <form
        aria-label="Crear preset"
        className="flex flex-wrap items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          onSave(name);
          setName("");
        }}
      >
        <TextInput aria-label="Nombre del preset (opcional)" placeholder="Nombre (opcional)" value={name} maxLength={128} disabled={!!reasons.presets} onChange={(e) => setName(e.target.value)} />
        <Button size="sm" type="submit" disabled={busy || !!reasons.presets} title={reasons.presets}>Crear preset</Button>
        <span className="text-[11px] text-muted">Guarda la posición actual de la cámara.</span>
      </form>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={busy || !!reasons.goHome} title={reasons.goHome} onClick={onGoHome}>Ir a home</Button>
        <Button size="sm" disabled={busy || !!reasons.setHome} title={reasons.setHome} onClick={onSetHome}>Guardar home</Button>
      </div>
      {shown.map((r) => <p key={r} className="text-xs text-muted">{r}</p>)}
    </section>
  );
}

function RenameForm({ initial, busy, onSubmit, onCancel }: { initial: string; busy: boolean; onSubmit: (name: string) => void; onCancel: () => void }) {
  const [value, setValue] = useState(initial);
  return (
    <form
      aria-label="Renombrar preset"
      className="flex w-full flex-wrap items-center gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(value);
      }}
    >
      <TextInput aria-label="Nuevo nombre" value={value} maxLength={128} onChange={(e) => setValue(e.target.value)} />
      <Button size="sm" variant="primary" type="submit" disabled={busy || value.trim() === ""}>Guardar nombre</Button>
      <Button size="sm" onClick={onCancel}>Cancelar</Button>
      <span className="w-full text-[11px] text-muted">Renombrar también vuelve a guardar la posición actual de la cámara en este preset.</span>
    </form>
  );
}
