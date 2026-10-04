import { SlidersHorizontal } from "lucide-react";
import { Icon } from "./Icon";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cameraFrigateConfigQuery } from "@/api/queries";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Field, Select, StatusBadge, Switch, TextInput } from "@/components/ui";

type Camera = Schemas["Camera"];

function FrigateSummary({ config }: { config: Schemas["CameraFrigateConfig"] }) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
      <dt className="text-muted">Zonas</dt>
      <dd>{config.zones.length > 0 ? config.zones.join(", ") : "Sin zonas"}</dd>
      <dt className="text-muted">Detección</dt>
      <dd>{config.detect_enabled ? "Habilitada" : "Deshabilitada"}</dd>
      <dt className="text-muted">LPR</dt>
      <dd>{config.lpr_enabled ? "Habilitado" : "Deshabilitado"}</dd>
      <dt className="text-muted">Objetos</dt>
      <dd>{config.tracked_objects.join(", ") || "Ninguno"}</dd>
    </dl>
  );
}

/**
 * CameraSettingsDrawer shows a camera's read-only inventory context and, for users
 * who can manage cameras or configure servers, editing forms for VMS-side settings
 * and a link to the full Frigate config editor (FrigateCameraConfig).
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

  // Read-only Frigate summary; the full editor lives on its own page (FrigateCameraConfig).
  const frigateConfig = useQuery(cameraFrigateConfigQuery(camera.id));

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
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras"] });
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
              <Switch label="Habilitada" checked={enabled} onChange={setEnabled} />
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

          <div className="mt-2 flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-4">
            <h3 className="mb-2 text-base font-bold">Configuración en Frigate</h3>
            {frigateConfig.isLoading && <p className="text-xs text-muted">Cargando configuración de Frigate…</p>}
            {frigateConfig.error && <p className="text-xs text-muted">Configuración de Frigate no disponible.</p>}
            {frigateConfig.data && <FrigateSummary config={frigateConfig.data} />}
            {canConfigServer && (
              <Link
                to="/cameras/$cameraId/frigate"
                params={{ cameraId: camera.id }}
                onClick={onClose}
                className="m3-press mt-3 inline-flex h-11 items-center gap-2 self-start rounded-full bg-secondary-container px-5 text-sm font-bold text-on-secondary-container hover:brightness-110 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
              >
                <Icon icon={SlidersHorizontal} size="xs" /> Editar configuración de Frigate
              </Link>
            )}
          </div>

          <ErrorNote error={save.error} />
          <div className="flex justify-end gap-2 mt-2">
            <Button variant="text" onClick={onClose}>Cancelar</Button>
            <Button type="submit" variant="primary" disabled={save.isPending}>
              {save.isPending ? "Guardando…" : "Guardar"}
            </Button>
          </div>
        </form>
      ) : (
        <div className="flex flex-col gap-3">
          <p className="text-xs text-muted">Necesitas el permiso cameras.manage o servers.config para editar esta cámara.</p>
          {frigateConfig.data && (
            <div className="rounded-m3-lg bg-surface-2 p-4">
              <h3 className="mb-2 text-base font-bold">Configuración en Frigate</h3>
              <FrigateSummary config={frigateConfig.data} />
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}
