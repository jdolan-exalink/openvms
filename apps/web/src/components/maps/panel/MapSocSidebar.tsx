import { useInfiniteQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Search } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { platesQuery } from "@/api/queries";
import { VehicleFacts } from "@/components/VehicleMark";
import { ArPlate } from "@/components/plates/ArPlate";
import { cn } from "@/lib/cn";
import { fmtDateTime } from "@/lib/format";
import { ProtectedGallery } from "./ProtectedGallery";

type Tab = "cameras" | "alarms" | "lpr" | "saved";

export function MapSocSidebar({
  open,
  onToggle,
  alarmCount,
  showAlarms,
  showLpr,
  siteId,
  onSelectCamera,
  onOpenRead,
  cameras,
  alarms,
}: {
  open: boolean;
  onToggle: () => void;
  alarmCount: number;
  showAlarms: boolean;
  showLpr: boolean;
  siteId?: string;
  onSelectCamera: (cameraId: string) => void;
  onOpenRead?: (read: { id: string; plate: string; cameraName: string; seenAt: string; imageUrl: string; origin: { left: number; top: number; width: number; height: number } }) => void;
  cameras: ReactNode;
  alarms: ReactNode;
}) {
  const [tab, setTab] = useState<Tab>("cameras");
  return (
    <div className="pointer-events-none absolute bottom-3 left-3 top-16 z-20 flex">
      <aside
        id="map-soc-sidebar"
        aria-label="Notificaciones del mapa"
        aria-hidden={!open || undefined}
        className={cn(
          "pointer-events-auto flex h-full w-80 min-h-0 flex-col overflow-hidden rounded-xl border border-white/10 bg-surface/90 shadow-2xl backdrop-blur-md transition-[width,opacity] duration-300",
          !open && "w-0 border-0 opacity-0",
        )}
      >
        <div className="flex shrink-0 border-b border-line text-xs font-medium" role="tablist" aria-label="Secciones del mapa">
          <TabButton id="cameras" current={tab} onSelect={setTab}>Cámaras</TabButton>
          <TabButton id="alarms" current={tab} onSelect={setTab}>
            Alarmas
            <AlarmCount count={showAlarms ? alarmCount : 0} />
          </TabButton>
          <TabButton id="lpr" current={tab} onSelect={setTab}>LPR</TabButton>
          <TabButton id="saved" current={tab} onSelect={setTab}>Protegidos</TabButton>
        </div>
        <div className="min-h-0 flex-1 space-y-2 overflow-auto p-2">
          {tab === "cameras" && cameras}
          {tab === "alarms" && (showAlarms ? alarms : <p className="p-2 text-xs text-muted">Las alarmas requieren permiso de alarmas.</p>)}
          {tab === "lpr" && (showLpr ? <LprList siteId={siteId} onSelectCamera={onSelectCamera} onOpenRead={onOpenRead} /> : <p className="p-2 text-xs text-muted">Las patentes requieren permiso de LPR.</p>)}
          {tab === "saved" && <ProtectedGallery />}
        </div>
      </aside>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        aria-controls="map-soc-sidebar"
        title={open ? "Contraer barra" : "Desplegar barra"}
        aria-label={open ? "Contraer barra" : "Desplegar barra"}
        className="pointer-events-auto absolute -right-3.5 top-16 flex size-7 items-center justify-center rounded-full border border-white/20 bg-raised text-muted shadow-xl hover:bg-accent hover:text-white"
      >
        {open ? <ChevronLeft className="size-3.5" aria-hidden /> : <ChevronRight className="size-3.5" aria-hidden />}
      </button>
    </div>
  );
}

function TabButton({ id, current, onSelect, children }: { id: Tab; current: Tab; onSelect: (tab: Tab) => void; children: ReactNode }) {
  const selected = current === id;
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      onClick={() => onSelect(id)}
      className={cn("flex flex-1 items-center justify-center gap-1 whitespace-nowrap px-1 py-2.5 text-[11px]", selected ? "border-b-2 border-accent text-accent" : "text-muted hover:text-ink")}
    >
      {children}
    </button>
  );
}

function AlarmCount({ count }: { count: number }) {
  if (count <= 0) return null;
  return <span className="rounded-full bg-bad/20 px-1 text-[10px] leading-4 text-bad">{count > 99 ? "99+" : count}</span>;
}

const LPR_PAGE = 8;

function LprList({ siteId, onSelectCamera, onOpenRead }: { siteId?: string; onSelectCamera: (cameraId: string) => void; onOpenRead?: (read: { id: string; plate: string; cameraName: string; seenAt: string; imageUrl: string; origin: { left: number; top: number; width: number; height: number } }) => void }) {
  const [draft, setDraft] = useState("");
  const [plate, setPlate] = useState("");
  const [page, setPage] = useState(0);
  useEffect(() => {
    const timer = window.setTimeout(() => setPlate(draft.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [draft]);
  useEffect(() => setPage(0), [plate, siteId]);
  const reads = useInfiniteQuery({
    ...platesQuery({ site_id: siteId ? [siteId] : undefined, plate: plate || undefined, limit: LPR_PAGE }),
    enabled: !!siteId,
  });
  const pages = reads.data?.pages ?? [];
  const items = pages[page]?.items ?? [];
  const lastPage = Math.max(0, pages.length - 1);
  const canNext = page < lastPage || !!reads.hasNextPage;
  async function goNext() {
    if (page < lastPage) {
      setPage(page + 1);
      return;
    }
    if (!reads.hasNextPage || reads.isFetchingNextPage) return;
    await reads.fetchNextPage();
    setPage(page + 1);
  }
  if (!siteId) return <p className="p-2 text-xs text-muted">Elegí un sitio para ver las patentes.</p>;
  return (
    <div className="space-y-2">
      <label className="relative block">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted" aria-hidden />
        <span className="sr-only">Buscar patente</span>
        <input
          type="search"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          placeholder="Buscar patente"
          aria-label="Buscar patente"
          className="w-full rounded-lg border border-line bg-bg py-1.5 pl-8 pr-2 text-xs"
        />
      </label>
      {reads.isError && <p role="alert" className="p-2 text-xs text-bad">No se pudieron cargar las patentes.</p>}
      {reads.isLoading && <p role="status" className="p-2 text-xs text-muted">Cargando patentes…</p>}
      {!reads.isLoading && !reads.isError && !items.length && (
        <p className="p-2 text-xs text-muted">{plate ? "No hay patentes con esa búsqueda." : "No hay lecturas de patentes en este sitio."}</p>
      )}
      {items.length > 0 && (
        <ul className="space-y-2" aria-label="Lecturas LPR">
          {items.map((read) => (
            <li key={read.id}>
              <button type="button" onClick={(event) => {
                const rect = event.currentTarget.getBoundingClientRect();
                const text = read.plate_normalized || read.plate;
                onOpenRead?.({ id: read.id, plate: text, cameraName: read.camera_name, seenAt: read.seen_at, imageUrl: `/media/v1/lpr/reads/${encodeURIComponent(read.id)}/snapshot.jpg`, origin: { left: rect.left, top: rect.top, width: rect.width, height: rect.height } });
                onSelectCamera(read.camera_id);
              }} className="flex w-full items-center gap-2 rounded-xl border border-line bg-bg/40 p-2 text-left hover:border-accent/40">
                <ArPlate plate={read.plate_normalized || read.plate} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[11px] text-ink">{read.camera_name}</span>
                  <VehicleFacts labels={read.label ? [read.label] : []} vehicle={read.vehicle} serverName={read.server_name} />
                  <span className="block text-[10px] text-muted">{fmtDateTime(read.seen_at)}</span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {(items.length > 0 || page > 0) && (
        <div className="flex items-center justify-between gap-2 pt-1 text-[11px]">
          <button type="button" className="rounded border border-line px-2 py-1 disabled:opacity-40" disabled={page === 0} onClick={() => setPage(page - 1)}>Anterior</button>
          <span className="text-muted">Página {page + 1}</span>
          <button type="button" className="rounded border border-line px-2 py-1 disabled:opacity-40" disabled={!canNext || reads.isFetchingNextPage} onClick={() => void goNext()}>Siguiente</button>
        </div>
      )}
    </div>
  );
}
