import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cameraFrigateConfigQuery } from "@/api/queries";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Field, Select, StatusBadge, TextInput } from "@/components/ui";

type Camera = Schemas["Camera"];

/**
 * CameraSettingsDrawer shows a camera's read-only inventory context and, for users
 * who can manage cameras or configure servers, editing forms for VMS-side settings
 * and Frigate-side analytics settings (detection, tracked objects, LPR).
 */
export function CameraSettingsDrawer({
  camera,
  siteName,
  serverName,
  canManage,
  canConfigServer = false,
  onClose,
}: {
  camera: Camera;
  siteName?: string;
  serverName?: string;
  canManage: boolean;
  canConfigServer?: boolean;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const [name, setName] = useState(camera.display_name);
  const [enabled, setEnabled] = useState(camera.enabled);
  const [quality, setQuality] = useState<Camera["default_live_quality"]>(camera.default_live_quality);
  const [description, setDescription] = useState(camera.description);
  const [location, setLocation] = useState(camera.location);
  const [tags, setTags] = useState(camera.tags.join(", "));
  const [invalid, setInvalid] = useState(false);

  // Frigate config
  const frigateConfig = useQuery(cameraFrigateConfigQuery(camera.id));
  const [detectEnabled, setDetectEnabled] = useState(true);
  const [lprEnabled, setLprEnabled] = useState(false);
  const [trackedObjects, setTrackedObjects] = useState("");
  const [frigateInitialized, setFrigateInitialized] = useState(false);

  if (frigateConfig.data && !frigateInitialized) {
    setDetectEnabled(frigateConfig.data.detect_enabled);
    setLprEnabled(frigateConfig.data.lpr_enabled);
    setTrackedObjects(frigateConfig.data.tracked_objects.join(", "));
    setFrigateInitialized(true);
  }

  const save = useMutation({
    mutationFn: async () => {
      if (canManage) {
        await unwrap(
          await api.PATCH("/api/v1/cameras/{cameraId}", {
            params: { path: { cameraId: camera.id } },
            body: {
              display_name: name.trim(),
              enabled,
              default_live_quality: quality,
              description: description.trim(),
              location: location.trim(),
              tags: tags.split(",").map((t) => t.trim()).filter(Boolean),
            },
          }),
        );
      }
      if (canConfigServer && frigateConfig.data) {
        await unwrap(
          await api.PATCH("/api/v1/cameras/{cameraId}/config", {
            params: { path: { cameraId: camera.id } },
            body: {
              detect_enabled: detectEnabled,
              lpr_enabled: lprEnabled,
              tracked_objects: trackedObjects.split(",").map((t) => t.trim()).filter(Boolean),
            },
          }),
        );
      }
    },
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["cameras"] }),
        qc.invalidateQueries({ queryKey: ["cameras", camera.id, "frigate-config"] }),
      ]);
      onClose();
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    if (canManage && !name.trim()) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    save.mutate();
  }

  const canEdit = canManage || canConfigServer;

  return (
    <Modal title="Ajustes de cámara" onClose={onClose}>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        <dt className="text-muted">Origen en Frigate</dt>
        <dd className="font-mono text-xs">{camera.remote_name}</dd>
        <dt className="text-muted">Sitio</dt>
        <dd>{siteName ?? "—"}</dd>
        <dt className="text-muted">Servidor</dt>
        <dd>{serverName ?? "—"}</dd>
        <dt className="text-muted">Estado</dt>
        <dd>{camera.enabled ? <StatusBadge status={camera.status} /> : "Deshabilitada"}</dd>
      </dl>

      {canEdit ? (
        <form onSubmit={submit} className="flex flex-col gap-3">
          {canManage ? (
            <>
              <Field label="Nombre">
                <TextInput value={name} onChange={(e) => setName(e.target.value)} aria-invalid={invalid} />
              </Field>
              {invalid && <p role="alert" className="text-xs text-bad">El nombre no puede estar vacío.</p>}
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
                Habilitada
              </label>
              <Field label="Calidad en vivo por defecto" hint="Flujo que usa la cámara al añadirla a la grilla de Vivo.">
                <Select value={quality} onChange={(e) => setQuality(e.target.value as Camera["default_live_quality"])}>
                  <option value="sub">Sub (menor calidad)</option>
                  <option value="main">Main (alta calidad)</option>
                </Select>
              </Field>
              <Field label="Descripción">
                <TextInput value={description} maxLength={1000} onChange={(e) => setDescription(e.target.value)} />
              </Field>
              <Field label="Ubicación">
                <TextInput value={location} maxLength={200} onChange={(e) => setLocation(e.target.value)} />
              </Field>
              <Field label="Etiquetas" hint="Separadas por comas (máximo 20).">
                <TextInput value={tags} onChange={(e) => setTags(e.target.value)} />
              </Field>
            </>
          ) : (
            <p className="text-xs text-muted">Necesitas cameras.manage para editar los ajustes de VMS.</p>
          )}

          <div className="mt-2 border-t border-line pt-3">
            <h3 className="text-sm font-semibold text-foreground mb-2">Configuración en Frigate</h3>
            {frigateConfig.isLoading && <p className="text-xs text-muted">Cargando configuración de Frigate…</p>}
            {frigateConfig.error && <p className="text-xs text-muted">Configuración de Frigate no disponible.</p>}
            {frigateConfig.data && (
              <div className="flex flex-col gap-3">
                <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                  <dt className="text-muted">Zonas</dt>
                  <dd>{frigateConfig.data.zones.length > 0 ? frigateConfig.data.zones.join(", ") : "Sin zonas"}</dd>
                </dl>
                {canConfigServer ? (
                  <>
                    <label className="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        checked={detectEnabled}
                        onChange={(e) => setDetectEnabled(e.target.checked)}
                      />
                      Detección habilitada
                    </label>
                    <label className="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        checked={lprEnabled}
                        onChange={(e) => setLprEnabled(e.target.checked)}
                      />
                      LPR habilitado
                    </label>
                    <Field label="Objetos rastreados" hint="Separados por comas (ej. person, car, dog).">
                      <TextInput
                        value={trackedObjects}
                        onChange={(e) => setTrackedObjects(e.target.value)}
                      />
                    </Field>
                  </>
                ) : (
                  <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                    <dt className="text-muted">Detección</dt>
                    <dd>{frigateConfig.data.detect_enabled ? "Habilitada" : "Deshabilitada"}</dd>
                    <dt className="text-muted">LPR</dt>
                    <dd>{frigateConfig.data.lpr_enabled ? "Habilitado" : "Deshabilitado"}</dd>
                    <dt className="text-muted">Objetos</dt>
                    <dd>{frigateConfig.data.tracked_objects.join(", ") || "Ninguno"}</dd>
                  </dl>
                )}
              </div>
            )}
          </div>

          <ErrorNote error={save.error} />
          <div className="flex justify-end gap-2 mt-2">
            <Button onClick={onClose}>Cancelar</Button>
            <Button type="submit" variant="primary" disabled={save.isPending}>
              {save.isPending ? "Guardando…" : "Guardar"}
            </Button>
          </div>
        </form>
      ) : (
        <div className="flex flex-col gap-3">
          <p className="text-xs text-muted">Necesitas el permiso cameras.manage o servers.config para editar esta cámara.</p>
          {frigateConfig.data && (
            <div className="border-t border-line pt-2">
              <h3 className="text-sm font-semibold mb-2">Configuración en Frigate</h3>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                <dt className="text-muted">Zonas</dt>
                <dd>{frigateConfig.data.zones.length > 0 ? frigateConfig.data.zones.join(", ") : "Sin zonas"}</dd>
                <dt className="text-muted">Detección</dt>
                <dd>{frigateConfig.data.detect_enabled ? "Habilitada" : "Deshabilitada"}</dd>
                <dt className="text-muted">LPR</dt>
                <dd>{frigateConfig.data.lpr_enabled ? "Habilitado" : "Deshabilitado"}</dd>
                <dt className="text-muted">Objetos</dt>
                <dd>{frigateConfig.data.tracked_objects.join(", ") || "Ninguno"}</dd>
              </dl>
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}
