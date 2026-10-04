import { useInfiniteQuery } from "@tanstack/react-query";
import { ChevronsRight, Pin, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode, type FocusEvent } from "react";
import { platesQuery } from "@/api/queries";
import { Icon } from "@/components/Icon";
import { Button, IconButton, Select, TextInput } from "@/components/ui";
import { PlateReadCard, plateCropUrl } from "@/components/plates/PlateReadCard";
import { cn } from "@/lib/cn";
import { vehicleColorOptions, vehicleTypeOptions } from "@/lib/format";
import { useT } from "@/i18n";
import { ProtectedGallery } from "./ProtectedGallery";

type Tab = "cameras" | "alarms" | "lpr" | "saved";

export function MapSocSidebar({
  pinned,
  onTogglePin,
  alarmCount,
  showAlarms,
  showLpr,
  siteId,
  onSelectCamera,
  onOpenRead,
  cameras,
  alarms,
}: {
  pinned: boolean;
  onTogglePin: () => void;
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
  const [edgeOpen, setEdgeOpen] = useState(false);
  const edgeHover = useRef(false);
  const hideEdge = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const open = pinned || edgeOpen;
  const reveal = () => {
    edgeHover.current = true;
    clearTimeout(hideEdge.current);
    setEdgeOpen(true);
  };
  const conceal = () => {
    edgeHover.current = false;
    clearTimeout(hideEdge.current);
    hideEdge.current = setTimeout(() => {
      if (!edgeHover.current && !pinned) setEdgeOpen(false);
    }, 280);
  };
  const onBlur = (event: FocusEvent<HTMLDivElement>) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) conceal();
  };
  useEffect(() => () => clearTimeout(hideEdge.current), []);
  return (
    <div
      data-testid="maps-sidebar-edge"
      onPointerEnter={reveal}
      onPointerLeave={conceal}
      onFocus={reveal}
      onBlur={onBlur}
      className={cn("pointer-events-auto absolute bottom-3 left-3 top-16 z-20 flex", open ? "w-80 max-w-[85vw]" : "w-3")}
    >
      <aside
        id="map-soc-sidebar"
        aria-label={t("maps.notifications")}
        aria-hidden={!open || undefined}
        hidden={!open}
        className="flex h-full w-80 min-h-0 flex-col overflow-hidden rounded-m3-xl bg-surface-1/95 shadow-2xl backdrop-blur-md"
      >
        <div className="flex shrink-0 items-center gap-1 p-2 text-xs font-medium" role="tablist" aria-label={t("maps.sections")}>
          <TabButton id="cameras" current={tab} onSelect={setTab}>{t("maps.cameras")}</TabButton>
          <TabButton id="alarms" current={tab} onSelect={setTab}>
            {t("maps.alarms")}
            <AlarmCount count={showAlarms ? alarmCount : 0} />
          </TabButton>
          <TabButton id="lpr" current={tab} onSelect={setTab}>{t("maps.lpr")}</TabButton>
          <TabButton id="saved" current={tab} onSelect={setTab}>{t("maps.saved")}</TabButton>
          <IconButton
            icon={Pin}
            variant={pinned ? "tonal" : "standard"}
            onClick={onTogglePin}
            aria-pressed={pinned}
            title={pinned ? t("live.unpin") : t("live.pin")}
            aria-label={pinned ? t("live.unpin") : t("live.pin")}
            className="size-11 shrink-0"
          />
        </div>
        <div className={cn("min-h-0 flex-1", tab === "lpr" ? "flex flex-col overflow-hidden" : "space-y-2 overflow-auto p-2")}>
          {tab === "cameras" && cameras}
          {tab === "alarms" && (showAlarms ? alarms : <p className="p-2 text-xs text-on-surface-variant">{t("maps.alarmsNeedPermission")}</p>)}
          {tab === "lpr" && (showLpr ? <LprList siteId={siteId} onSelectCamera={onSelectCamera} onOpenRead={onOpenRead} /> : <p className="p-2 text-xs text-on-surface-variant">{t("maps.platesNeedPermission")}</p>)}
          {tab === "saved" && <ProtectedGallery />}
        </div>
      </aside>
      {!open && (
        <button type="button" aria-label={t("live.showExplorer")} title={t("live.showExplorer")} className="absolute inset-0 flex items-center justify-center rounded-r-m3-md bg-surface-1/95 text-on-surface-variant hover:bg-surface-2" onClick={reveal}>
          <Icon icon={ChevronsRight} size="xs" />
        </button>
      )}
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
      className={cn("m3-press flex h-11 flex-1 items-center justify-center gap-1 whitespace-nowrap rounded-full px-1 text-[11px] font-bold focus-visible:outline-2 focus-visible:outline-primary", selected ? "bg-primary-container text-on-primary-container" : "text-on-surface-variant hover:bg-on-surface/8")}
    >
      {children}
    </button>
  );
}

function AlarmCount({ count }: { count: number }) {
  if (count <= 0) return null;
  return <span className="rounded-full bg-bad/20 px-1.5 font-mono text-[10px] leading-4 text-bad">{count > 99 ? "99+" : count}</span>;
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
  if (!siteId) return <p className="p-2 text-xs text-on-surface-variant">{t("maps.pickSite")}</p>;
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2 p-2">
      <label className="relative block">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-on-surface-variant" aria-hidden />
        <span className="sr-only">{t("maps.searchPlate")}</span>
        <TextInput
          type="search"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          placeholder={t("maps.searchPlate")}
          aria-label={t("maps.searchPlate")}
          className="pl-10 font-mono text-xs"
        />
      </label>
      <div className="grid grid-cols-2 gap-2">
        <Select aria-label={t("common.object")} value={vehicleType} onChange={(event) => setVehicleType(event.target.value)} className="text-xs">
          <option value="">{t("common.object")}</option>
          {vehicleTypeOptions().map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </Select>
        <Select aria-label={t("common.color")} value={vehicleColor} onChange={(event) => setVehicleColor(event.target.value)} className="text-xs">
          <option value="">{t("common.color")}</option>
          {vehicleColorOptions().map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </Select>
      </div>
      {reads.isError && <p role="alert" className="p-2 text-xs text-bad">{t("maps.loadFailed")}</p>}
      {reads.isLoading && <p role="status" className="p-2 text-xs text-on-surface-variant">{t("maps.loadingPlates")}</p>}
      {!reads.isLoading && !reads.isError && !items.length && (
        <p className="p-2 text-xs text-on-surface-variant">{plate ? t("maps.noSearchToday") : t("maps.noReadsToday")}</p>
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
      <div className="mt-auto flex shrink-0 items-center justify-between gap-2 pt-2 text-[11px]">
        <Button variant="outlined" size="sm" disabled={page === 0} onClick={() => setPage(page - 1)}>{t("common.previous")}</Button>
        <span className="font-mono text-on-surface-variant">{t("maps.todayPage", { page: page + 1 })}</span>
        <Button variant="outlined" size="sm" disabled={!canNext || reads.isFetchingNextPage} onClick={() => void goNext()}>{t("common.next")}</Button>
      </div>
    </div>
  );
}
