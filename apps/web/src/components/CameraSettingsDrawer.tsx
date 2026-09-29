import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Field, Select, StatusBadge, TextInput } from "@/components/ui";

type Camera = Schemas["Camera"];

/**
 * CameraSettingsDrawer shows a camera's read-only inventory context and, only for users
 * who can manage cameras, a form for the fields PATCH /api/v1/cameras/{id} accepts
 * (display_name, enabled and the VMS-side default live quality, description, location and
 * tags). The API stays authoritative for authorization and validation.
 */
export function CameraSettingsDrawer({
  camera,
  siteName,
  serverName,
  canManage,
  onClose,
}: {
  camera: Camera;
  siteName?: string;
  serverName?: string;
  canManage: boolean;
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
  const save = useMutation({
    mutationFn: async () =>
      unwrap(
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
      ),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras"] });
      onClose();
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    save.mutate();
  }

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
      {canManage ? (
        <form onSubmit={submit} className="flex flex-col gap-3">
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
          <ErrorNote error={save.error} />
          <div className="flex justify-end gap-2">
            <Button onClick={onClose}>Cancelar</Button>
            <Button type="submit" variant="primary" disabled={save.isPending}>
              {save.isPending ? "Guardando…" : "Guardar"}
            </Button>
          </div>
        </form>
      ) : (
        <p className="text-xs text-muted">Necesitas el permiso cameras.manage para editar esta cámara.</p>
      )}
    </Modal>
  );
}
