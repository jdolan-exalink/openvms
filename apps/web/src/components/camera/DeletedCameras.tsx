import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, type Schemas } from "@/api/client";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote } from "@/components/ui";

type Deleted = Schemas["DeletedCamera"];

const deletedQuery = (serverId: string) => ({
  queryKey: ["deleted-cameras", serverId],
  queryFn: async () => unwrap(await api.GET("/api/v1/servers/{serverId}/deleted-cameras", { params: { path: { serverId } } })).items,
});

const when = (iso: string) => new Date(iso).toLocaleString("es-AR", { dateStyle: "medium", timeStyle: "short" });

/** Cameras deleted from a server that can still be restored with their history. Needs servers.manage. */
export function DeletedCameras({ serverId }: { serverId: string }) {
  const deleted = useQuery(deletedQuery(serverId));
  const [target, setTarget] = useState<Deleted | null>(null);
  if (!deleted.data?.length) return <ErrorNote error={deleted.error} />;
  return (
    <section aria-label="Cámaras eliminadas" className="flex flex-col gap-2 rounded-m3-xl bg-surface-1 p-4">
      <h3 className="text-sm font-semibold">Cámaras eliminadas</h3>
      <ul className="flex flex-col gap-2">
        {deleted.data.map((d) => (
          <li key={d.id} className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span>
              {d.display_name} <span className="text-muted">({d.remote_name}) · eliminada el {when(d.deleted_at)}</span>
            </span>
            <Button variant="tonal" onClick={() => setTarget(d)}>Restaurar</Button>
          </li>
        ))}
      </ul>
      {target && <RestoreDialog camera={target} serverId={serverId} onClose={() => setTarget(null)} />}
    </section>
  );
}

function RestoreDialog({ camera, serverId, onClose }: { camera: Deleted; serverId: string; onClose: () => void }) {
  const qc = useQueryClient();
  const restore = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/cameras/{cameraId}/restore", { params: { path: { cameraId: camera.id } } })),
    onSuccess: async () => {
      await Promise.all([qc.invalidateQueries({ queryKey: ["cameras"] }), qc.invalidateQueries({ queryKey: deletedQuery(serverId).queryKey })]);
      onClose();
    },
  });
  return (
    <Modal title="Restaurar cámara" onClose={onClose}>
      <p className="text-sm">
        Se restaurará "{camera.display_name}" con su mismo historial: el historial anterior se unirá con las grabaciones nuevas, sin pisarlo.
        {camera.has_frigate_config
          ? " Se volverá a agregar a la configuración de Frigate, que se reiniciará."
          : " No se guardó su configuración de Frigate, así que solo se restaura en OpenVMS: si Frigate no la tiene, no grabará."}
      </p>
      <ErrorNote error={restore.error} />
      <div className="flex justify-end gap-2">
        <Button variant="text" onClick={onClose}>Cancelar</Button>
        <Button variant="filled" disabled={restore.isPending} onClick={() => restore.mutate()}>Restaurar</Button>
      </div>
    </Modal>
  );
}
