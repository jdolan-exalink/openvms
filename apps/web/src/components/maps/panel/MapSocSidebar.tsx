import { useInfiniteQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Search } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { platesQuery } from "@/api/queries";
import { PlateReadCard, plateCropUrl } from "@/components/plates/PlateReadCard";
import { cn } from "@/lib/cn";
import { vehicleColorOptions, vehicleTypeOptions } from "@/lib/format";
import { useT } from "@/i18n";
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
  const t = useT();
  const [tab, setTab] = useState<Tab>("cameras");
  return (
    <div className="pointer-events-none absolute bottom-3 left-3 top-16 z-20 flex">
      <aside
        id="map-soc-sidebar"
        aria-label={t("maps.notifications")}
        aria-hidden={!open || undefined}
        className={cn(
          "pointer-events-auto flex h-full w-80 min-h-0 flex-col overflow-hidden rounded-xl border border-white/10 bg-surface/90 shadow-2xl backdrop-blur-md transition-[width,opacity] duration-300",
          !open && "w-0 border-0 opacity-0",
        )}
      >
        <div className="flex shrink-0 border-b border-line text-xs font-medium" role="tablist" aria-label={t("maps.sections")}>
          <TabButton id="cameras" current={tab} onSelect={setTab}>{t("maps.cameras")}</TabButton>
          <TabButton id="alarms" current={tab} onSelect={setTab}>
            {t("maps.alarms")}
            <AlarmCount count={showAlarms ? alarmCount : 0} />
          </TabButton>
          <TabButton id="lpr" current={tab} onSelect={setTab}>{t("maps.lpr")}</TabButton>
          <TabButton id="saved" current={tab} onSelect={setTab}>{t("maps.saved")}</TabButton>
        </div>
        <div className={cn("min-h-0 flex-1", tab === "lpr" ? "flex flex-col overflow-hidden" : "space-y-2 overflow-auto p-2")}>
          {tab === "cameras" && cameras}
          {tab === "alarms" && (showAlarms ? alarms : <p className="p-2 text-xs text-muted">{t("maps.alarmsNeedPermission")}</p>)}
          {tab === "lpr" && (showLpr ? <LprList siteId={siteId} onSelectCamera={onSelectCamera} onOpenRead={onOpenRead} /> : <p className="p-2 text-xs text-muted">{t("maps.platesNeedPermission")}</p>)}
          {tab === "saved" && <ProtectedGallery />}
        </div>
      </aside>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        aria-controls="map-soc-sidebar"
        title={open ? t("maps.collapse") : t("maps.expand")}
        aria-label={open ? t("maps.collapse") : t("maps.expand")}
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

/** localDayBounds is today from 00:00 inclusive to the next midnight exclusive, in UTC instants. */
function localDayBounds(now = new Date()) {
  const start = new Date(now);
  start.setHours(0, 0, 0, 0);
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  return { from: start.toISOString(), to: end.toISOString() };
}

function LprList({ siteId, onSelectCamera, onOpenRead }: { siteId?: string; onSelectCamera: (cameraId: string) => void; onOpenRead?: (read: { id: string; plate: string; cameraName: string; seenAt: string; imageUrl: string; origin: { left: number; top: number; width: number; height: number } }) => void }) {
  const t = useT();
  const [draft, setDraft] = useState("");
  const [plate, setPlate] = useState("");
  const [vehicleType, setVehicleType] = useState("");
  const [vehicleColor, setVehicleColor] = useState("");
  const [page, setPage] = useState(0);
  const day = useMemo(() => localDayBounds(), []);
  useEffect(() => {
    const timer = window.setTimeout(() => setPlate(draft.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [draft]);
  useEffect(() => setPage(0), [plate, siteId, vehicleType, vehicleColor]);
  const reads = useInfiniteQuery({
    ...platesQuery({
      site_id: siteId ? [siteId] : undefined,
      plate: plate || undefined,
      vehicle_type: vehicleType ? [vehicleType] : undefined,
      vehicle_color: vehicleColor ? [vehicleColor] : undefined,
      from: day.from,
      to: day.to,
      limit: LPR_PAGE,
    }),
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
  if (!siteId) return <p className="p-2 text-xs text-muted">{t("maps.pickSite")}</p>;
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2 p-2">
      <label className="relative block">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted" aria-hidden />
        <span className="sr-only">{t("maps.searchPlate")}</span>
        <input
          type="search"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          placeholder={t("maps.searchPlate")}
          aria-label={t("maps.searchPlate")}
          className="w-full rounded-lg border border-line bg-bg py-1.5 pl-8 pr-2 text-xs"
        />
      </label>
      <div className="grid grid-cols-2 gap-1">
        <select aria-label={t("common.object")} value={vehicleType} onChange={(event) => setVehicleType(event.target.value)} className="rounded-lg border border-line bg-bg px-2 py-1 text-xs">
          <option value="">{t("common.object")}</option>
          {vehicleTypeOptions().map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
        <select aria-label={t("common.color")} value={vehicleColor} onChange={(event) => setVehicleColor(event.target.value)} className="rounded-lg border border-line bg-bg px-2 py-1 text-xs">
          <option value="">{t("common.color")}</option>
          {vehicleColorOptions().map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select>
      </div>
      {reads.isError && <p role="alert" className="p-2 text-xs text-bad">{t("maps.loadFailed")}</p>}
      {reads.isLoading && <p role="status" className="p-2 text-xs text-muted">{t("maps.loadingPlates")}</p>}
      {!reads.isLoading && !reads.isError && !items.length && (
        <p className="p-2 text-xs text-muted">{plate ? t("maps.noSearchToday") : t("maps.noReadsToday")}</p>
      )}
      {items.length > 0 && (
        <ul className="min-h-0 flex-1 space-y-2 overflow-auto" aria-label={t("maps.readings")}>
          {items.map((read) => (
            <PlateReadCard
              key={read.id}
              read={read}
              onClick={(origin) => {
                onOpenRead?.({
                  id: read.id,
                  plate: read.plate_normalized || read.plate,
                  cameraName: read.camera_name,
                  seenAt: read.seen_at,
                  imageUrl: plateCropUrl(read.id),
                  origin,
                });
                onSelectCamera(read.camera_id);
              }}
            />
          ))}
        </ul>
      )}
      <div className="mt-auto flex shrink-0 items-center justify-between gap-2 border-t border-line pt-2 text-[11px]">
        <button type="button" className="rounded border border-line px-2 py-1 disabled:opacity-40" disabled={page === 0} onClick={() => setPage(page - 1)}>{t("common.previous")}</button>
        <span className="text-muted">{t("maps.todayPage", { page: page + 1 })}</span>
        <button type="button" className="rounded border border-line px-2 py-1 disabled:opacity-40" disabled={!canNext || reads.isFetchingNextPage} onClick={() => void goNext()}>{t("common.next")}</button>
      </div>
    </div>
  );
}

