import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Field, StatusBadge, TextInput } from "@/components/ui";

type Camera = Schemas["Camera"];

/**
 * CameraSettingsDrawer shows a camera's read-only inventory context and, only for users
 * who can manage cameras, a form for the two fields PATCH /api/v1/cameras/{id} accepts
 * (display_name, enabled). The API stays authoritative for authorization.
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
  const [invalid, setInvalid] = useState(false);
  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PATCH("/api/v1/cameras/{cameraId}", {
          params: { path: { cameraId: camera.id } },
          body: { display_name: name.trim(), enabled },
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
          {invalid && <p className="text-xs text-bad">El nombre no puede estar vacío.</p>}
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            Habilitada
          </label>
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
