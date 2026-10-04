import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ShieldCheck, X } from "lucide-react";
import { Button, IconButton, Select } from "@/components/ui";
import { api, unwrap, type Schemas } from "@/api/client";
import { alarmAssigneesQuery } from "@/api/queries";
import { VehicleFacts } from "@/components/VehicleMark";
import { fmtDateTime } from "@/lib/format";
import { listProtectedImages, protectRemoteImage } from "@/lib/protectedImages";
import { MapGrowFrame, useGrowClose, type GrowRect } from "./MapGrowFrame";

type Action = "acknowledge" | "assign" | "investigate" | "resolve" | "close" | "comments";
const labels: Record<Action, string> = {
  acknowledge: "Marcar como vista",
  assign: "Asignar",
  investigate: "Investigar",
  resolve: "Resolver",
  close: "Cerrar alarma",
  comments: "Guardar comentario",
};
const statusLabel: Record<string, string> = {
  open: "Nueva",
  acknowledged: "Vista",
  assigned: "Asignada",
  investigating: "En revisión",
  resolved: "Resuelta",
  closed: "Cerrada",
};

export function AlarmPanel({ alarms, canManage }: { alarms: Schemas["Alarm"][]; canManage: boolean }) {
  const [selected, setSelected] = useState<{ alarm: Schemas["Alarm"]; origin?: GrowRect }>();
  const fresh = useFreshAlarmIds(alarms);
  return (
    <section aria-label="Alarms" className="space-y-2">
      {!alarms.length && <p className="p-2 text-xs text-on-surface-variant">No hay alarmas en este sitio</p>}
      <ul className="space-y-2">
        {alarms.map((item) => (
          <li key={item.id} className={fresh.has(item.id) ? "alarm-card-in" : undefined}>
            <AlarmCard alarm={item} onOpen={(origin) => setSelected({ alarm: item, origin })} />
          </li>
        ))}
      </ul>
      {selected && createPortal(
        <AlarmPreview
          alarm={selected.alarm}
          origin={selected.origin}
          canManage={canManage}
          onClose={() => setSelected(undefined)}
        />,
        document.body,
      )}
    </section>
  );
}

/** The first list is the current set. Later ids slide in once. */
function useFreshAlarmIds(alarms: Schemas["Alarm"][]) {
  const known = useRef<Set<string> | null>(null);
  const [fresh, setFresh] = useState<Set<string>>(() => new Set());
  const signature = alarms.map((alarm) => alarm.id).join("\0");
  useEffect(() => {
    const ids = signature ? signature.split("\0") : [];
    if (known.current === null) {
      known.current = new Set(ids);
      return;
    }
    const arrived = ids.filter((id) => !known.current?.has(id));
    for (const id of ids) known.current.add(id);
    if (!arrived.length) return;
    setFresh(new Set(arrived));
    const timer = window.setTimeout(() => setFresh(new Set()), 700);
    return () => window.clearTimeout(timer);
  }, [signature]);
  return fresh;
}

function AlarmCard({ alarm, onOpen }: { alarm: Schemas["Alarm"]; onOpen: (origin?: GrowRect) => void }) {
  const [imageFailed, setImageFailed] = useState(false);
  const snapshot = `/media/v1/events/${encodeURIComponent(alarm.event_id)}/snapshot.jpg`;
  const seen = alarm.status !== "open";
  return (
    <button
      type="button"
      onClick={(event) => {
        const rect = event.currentTarget.getBoundingClientRect();
        onOpen({ left: rect.left, top: rect.top, width: rect.width, height: rect.height });
      }}
      className="m3-press block w-full overflow-hidden rounded-m3-xl bg-surface-2 text-left ring-1 ring-bad/40 hover:ring-bad focus-visible:outline-2 focus-visible:outline-primary"
    >
      <span className="relative block h-24 bg-video">
        {imageFailed ? (
          <span className="flex h-full items-center justify-center text-[11px] text-on-surface-variant">Sin imagen</span>
        ) : (
          <img src={snapshot} alt="" className="size-full object-cover" onError={() => setImageFailed(true)} />
        )}
        <span className={`absolute left-2 top-2 rounded-full px-2.5 py-0.5 text-[10px] font-bold ${seen ? "bg-surface-2 text-on-surface-variant" : "bg-bad text-surface-dim light:text-white"}`}>
          {statusLabel[alarm.status] ?? alarm.status}
        </span>
      </span>
      <span className="block space-y-1 px-3 py-2">
        <span className="block truncate text-xs font-bold text-on-surface">{alarm.camera_name}</span>
        <VehicleFacts labels={alarm.event_labels} vehicle={alarm.vehicle} person={alarm.person} serverName={alarm.server_name} />
        <span className="block font-mono text-[10px] text-on-surface-variant">{fmtDateTime(alarm.created_at)}</span>
      </span>
    </button>
  );
}

function AlarmPreview({ alarm, origin, canManage, onClose }: { alarm: Schemas["Alarm"]; origin?: GrowRect; canManage: boolean; onClose: () => void }) {
  const [failed, setFailed] = useState(false);
  const snapshot = `/media/v1/events/${encodeURIComponent(alarm.event_id)}/snapshot.jpg`;
  return (
    <MapGrowFrame label={`Alarma ${alarm.camera_name}`} origin={origin} onClose={onClose} fixed className="z-50">
      <div className="relative aspect-video w-full bg-video">
        {failed ? (
          <p className="flex size-full items-center justify-center text-sm text-on-surface-variant">Imagen no disponible</p>
        ) : (
          <img src={snapshot} alt={`Alarma en ${alarm.camera_name}`} className="size-full object-contain" onError={() => setFailed(true)} />
        )}
        <div data-map-drag className="absolute inset-x-0 top-0 z-[3] flex cursor-grab items-center gap-2 bg-gradient-to-b from-black/80 to-transparent px-3 py-2 text-xs text-white active:cursor-grabbing">
          <span className="rounded-full bg-bad px-2.5 py-0.5 text-[10px] font-bold text-surface-dim light:text-white">{statusLabel[alarm.status] ?? alarm.status}</span>
          <span className="min-w-0 truncate font-medium">{alarm.camera_name}</span>
          <PreviewClose />
        </div>
      </div>
      <div className="px-3 py-2">
        <VehicleFacts labels={alarm.event_labels} vehicle={alarm.vehicle} person={alarm.person} serverName={alarm.server_name} />
      </div>
      <AlarmActions alarm={alarm} canManage={canManage} imageUrl={snapshot} />
    </MapGrowFrame>
  );
}

function PreviewClose() {
  const close = useGrowClose();
  return (
    <IconButton icon={X} onClick={close} className="ml-auto size-9 text-white hover:bg-white/20" aria-label="Cerrar vista de alarma" />
  );
}

function AlarmActions({ alarm, canManage, imageUrl }: { alarm: Schemas["Alarm"]; canManage: boolean; imageUrl: string }) {
  const client = useQueryClient();
  const [comment, setComment] = useState("");
  const [assignee, setAssignee] = useState("");
  const [protectError, setProtectError] = useState("");
  const saved = useQuery({ queryKey: ["protected-images"], queryFn: listProtectedImages });
  const protectedId = `alarm:${alarm.id}`;
  const alreadyProtected = saved.data?.some((item) => item.id === protectedId) ?? false;
  const history = useQuery({
    queryKey: ["alarms", alarm.id, "transitions"],
    queryFn: async () => unwrap(await api.GET("/api/v1/alarms/{alarmId}/transitions", { params: { path: { alarmId: alarm.id } } })),
  });
  const assignees = useQuery({ ...alarmAssigneesQuery(alarm.id), enabled: canManage });
  const action = useMutation({
    mutationFn: async (next: Action) => {
      if (!canManage) throw new Error("Alarm management permission required");
      const params = { path: { alarmId: alarm.id } };
      const note = comment.trim();
      switch (next) {
        case "acknowledge":
          await unwrap(await api.POST("/api/v1/alarms/{alarmId}/acknowledge", { params }));
          if (note) await unwrap(await api.POST("/api/v1/alarms/{alarmId}/comments", { params, body: { comment: note } }));
          return;
        case "resolve":
          return unwrap(await api.POST("/api/v1/alarms/{alarmId}/resolve", { params }));
        case "assign":
          return unwrap(await api.POST("/api/v1/alarms/{alarmId}/assign", { params, body: { user_id: assignee } }));
        case "investigate":
          return unwrap(await api.POST("/api/v1/alarms/{alarmId}/investigate", { params, body: { comment: note } }));
        case "close":
          return unwrap(await api.POST("/api/v1/alarms/{alarmId}/close", { params, body: { comment: note } }));
        case "comments":
          return unwrap(await api.POST("/api/v1/alarms/{alarmId}/comments", { params, body: { comment: note } }));
      }
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["alarms"] });
      void client.invalidateQueries({ queryKey: ["maps"] });
    },
  });
  const protect = useMutation({
    mutationFn: () => protectRemoteImage({
      id: protectedId,
      kind: "alarm",
      title: alarm.camera_name,
      detail: statusLabel[alarm.status] ?? alarm.status,
      comment,
      imageUrl,
      fullUrl: imageUrl,
    }),
    onSuccess: () => {
      setProtectError("");
      void client.invalidateQueries({ queryKey: ["protected-images"] });
    },
    onError: (error: Error) => setProtectError(error.message),
  });
  const legal = (next: Action) => {
    if (next === "comments") return !!comment.trim();
    if (alarm.status === "closed") return false;
    if (alarm.status === "resolved" && !["close", "investigate"].includes(next)) return false;
    if (next === "acknowledge" && alarm.status !== "open") return false;
    return next !== "assign" || !!assignee;
  };
  return (
    <div className="space-y-3 bg-surface-1 p-4 text-xs text-on-surface">
      {history.isError && <p role="alert" className="text-bad">{history.error.message}</p>}
      <ul className="max-h-16 space-y-0.5 overflow-auto font-mono text-[11px] text-on-surface-variant">
        {history.data?.map((item) => (
          <li key={item.id}>{item.at} {item.actor_name} {item.to_status} {item.comment}</li>
        ))}
      </ul>
      <Button variant="outlined" size="sm" disabled={alreadyProtected || protect.isPending} onClick={() => protect.mutate()}>
        <ShieldCheck className="size-4" aria-hidden />
        {alreadyProtected ? "Imagen protegida" : "Proteger imagen"}
      </Button>
      {protectError && <p role="alert" className="text-bad">{protectError}</p>}
      {protect.isSuccess && <p role="status">La copia quedó en Imágenes protegidas</p>}
      {canManage && (
        <div className="space-y-2">
          <label className="block">
            Comentario
            <textarea aria-label="Comment" value={comment} onChange={(event) => setComment(event.target.value)} className="mt-1 w-full rounded-m3-md border border-transparent bg-surface-2 px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-primary" rows={2} />
          </label>
          <label className="block">
            Responsable
            <Select aria-label="Assignee" value={assignee} onChange={(event) => setAssignee(event.target.value)} className="mt-1">
              <option value="">Elegir responsable</option>
              {assignees.data?.map((item) => <option key={item.id} value={item.id}>{item.display_name || item.username}</option>)}
            </Select>
          </label>
          {assignees.isError && <p role="alert" className="text-bad">{assignees.error.message}</p>}
          <div className="flex flex-wrap gap-2">
            {(Object.keys(labels) as Action[]).map((next) => (
              <Button key={next} variant={next === "close" ? "outlined" : "tonal"} size="sm" disabled={action.isPending || !legal(next)} onClick={() => action.mutate(next)}>
                {labels[next]}
              </Button>
            ))}
          </div>
          {action.isError && <p role="alert" className="text-bad">{action.error.message}</p>}
          {action.isSuccess && <p role="status">Acción registrada</p>}
        </div>
      )}
    </div>
  );
}
