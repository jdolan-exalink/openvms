import { useMutation, useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Icon } from "@/components/Icon";
import { Button } from "@/components/ui";

export type MoreAction = { id: string; label: string; onSelect: () => void; disabled?: boolean };

/** "Más acciones" menu of the header: secondary actions that must stay reachable. */
export function MoreActions({ actions }: { actions: MoreAction[] }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  return (
    <div ref={ref} className="relative">
      <Button variant="tonal" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <Icon icon={MoreHorizontal} size="xs" /> Más acciones
      </Button>
      {open && (
        <div role="menu" className="absolute right-0 top-full z-20 mt-1 flex min-w-56 flex-col rounded-m3-lg border border-outline-variant/60 bg-surface-1 p-1 shadow-lg">
          {actions.map((a) => (
            <button
              key={a.id}
              type="button"
              role="menuitem"
              disabled={a.disabled}
              onClick={() => { setOpen(false); a.onSelect(); }}
              className="min-h-10 rounded-m3-md px-3 text-left text-sm hover:bg-surface-2 disabled:opacity-50"
            >
              {a.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export const RESTART_CAMERA_LABEL = "Reiniciar cámara";
export const RESTART_CAMERA_WARNING = "La cámara dejará de transmitir mientras se reinicia.";

/** Confirmation of a camera restart (ONVIF SystemReboot); the request carries the explicit confirm flag. */
export function RestartCameraDialog({ cameraId, onClose }: { cameraId: string; onClose: () => void }) {
  const [done, setDone] = useState(false);
  const restart = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/cameras/{cameraId}/restart", { params: { path: { cameraId } }, body: { confirm: true } })),
    onSuccess: () => setDone(true),
  });
  if (done) {
    return <ConfirmDialog title={RESTART_CAMERA_LABEL} message="La cámara aceptó el reinicio. Volverá a transmitir en unos instantes." confirmLabel="Entendido" cancelLabel="Cerrar" onConfirm={onClose} onCancel={onClose} />;
  }
  return (
    <ConfirmDialog
      title={RESTART_CAMERA_LABEL}
      message={RESTART_CAMERA_WARNING}
      confirmLabel="Reiniciar"
      pending={restart.isPending}
      error={restart.error}
      onConfirm={() => restart.mutate()}
      onCancel={onClose}
    />
  );
}

export const DELETE_CAMERA_LABEL = "Eliminar cámara";

/**
 * Confirmation of a camera delete. The camera leaves OpenVMS and Frigate's config and stops recording;
 * its events and recordings are not deleted now, and it can be restored from the cameras list.
 */
export function DeleteCameraDialog({ cameraId, name, onClose, onDeleted }: { cameraId: string; name: string; onClose: () => void; onDeleted: () => void }) {
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/api/v1/cameras/{cameraId}", { params: { path: { cameraId } } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras"] });
      onDeleted();
    },
  });
  return (
    <ConfirmDialog
      title={DELETE_CAMERA_LABEL}
      message={`Se eliminará "${name}" de OpenVMS y de la configuración de Frigate, y dejará de grabar: no habrá grabaciones nuevas. Frigate se reiniciará, así que todas las cámaras de ese servidor dejarán de grabar unos instantes. OpenVMS no borra ahora sus eventos ni las grabaciones existentes: se conservan hasta que pasen sus días de grabación. Podés restaurarla desde la lista de cámaras del servidor.`}
      confirmLabel="Eliminar"
      pending={remove.isPending}
      error={remove.error}
      onConfirm={() => remove.mutate()}
      onCancel={onClose}
    />
  );
}
