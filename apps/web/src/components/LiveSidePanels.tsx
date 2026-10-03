import { useDraggable } from "@dnd-kit/core";
import { faMap } from "@fortawesome/free-solid-svg-icons";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { useInfiniteQuery, useQueries } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import type { Schemas } from "@/api/client";
import { eventsQuery, platesQuery } from "@/api/queries";
import { PlateReadCard } from "@/components/plates/PlateReadCard";
import { VehicleFacts } from "@/components/VehicleMark";
import { fmtDateTime } from "@/lib/format";
import { mapDragId, type LiveMapRef } from "@/lib/liveGrid";
import { useT } from "@/i18n";
import { siteDetailQuery } from "@/lib/maps/api";

const PAGE = 8;

/** Today from local midnight inclusive to the next midnight exclusive. */
function localDayBounds(now = new Date()) {
  const start = new Date(now);
  start.setHours(0, 0, 0, 0);
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  return { from: start.toISOString(), to: end.toISOString() };
}

function Pager({ page, canNext, pending, onPrev, onNext }: { page: number; canNext: boolean; pending: boolean; onPrev: () => void; onNext: () => void }) {
  const t = useT();
  return (
    <div className="mt-auto flex shrink-0 items-center justify-between gap-2 border-t border-line pt-2 text-[11px]">
      <button type="button" className="rounded border border-line px-2 py-1 disabled:opacity-40" disabled={page === 0} onClick={onPrev}>{t("common.previous")}</button>
      <span className="text-muted">{t("maps.todayPage", { page: page + 1 })}</span>
      <button type="button" className="rounded border border-line px-2 py-1 disabled:opacity-40" disabled={!canNext || pending} onClick={onNext}>{t("common.next")}</button>
    </div>
  );
}

/** Maps the operator can drop onto a live cell: the site map and each floor plan. */
export function LiveMapsPanel({ sites, canMaps, onPlace }: { sites: Schemas["Site"][]; canMaps: boolean; onPlace: (map: LiveMapRef) => void }) {
  const t = useT();
  const details = useQueries({
    queries: sites.map((site) => ({ ...siteDetailQuery(site.id), enabled: canMaps })),
  });
  if (!canMaps) return <p className="p-2 text-xs text-muted">{t("live.mapsNeedPermission")}</p>;
  if (!sites.length) return <p className="p-2 text-xs text-muted">{t("live.noSites")}</p>;
  const loading = details.some((query) => query.isLoading);
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-auto p-1">
      {loading && <p role="status" className="px-1 text-xs text-muted">{t("live.loadingMaps")}</p>}
      <ul className="space-y-3" aria-label={t("live.maps")}>
        {sites.map((site, index) => {
          const buildings = details[index]?.data?.buildings ?? [];
          const geographic: LiveMapRef = { site_id: site.id, name: site.name };
          return (
            <li key={site.id} className="space-y-1">
              <p className="truncate px-1 text-[11px] font-semibold uppercase tracking-wide text-muted">{site.name}</p>
              <MapRow map={geographic} hint={t("live.geographic")} onPlace={onPlace} />
              {buildings.flatMap((building) =>
                building.floors.map((floor) => (
                  <MapRow
                    key={floor.id}
                    map={{ site_id: site.id, floor_id: floor.id, name: `${building.name} / ${floor.name}` }}
                    hint={t("live.floorPlan")}
                    onPlace={onPlace}
                  />
                )),
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function MapRow({ map, hint, onPlace }: { map: LiveMapRef; hint: string; onPlace: (map: LiveMapRef) => void }) {
  const t = useT();
  const { setNodeRef, attributes, listeners, isDragging } = useDraggable({ id: mapDragId(map), data: { kind: "map", map } });
  return (
    <button
      ref={setNodeRef}
      type="button"
      title={t("live.mapDragHint", { name: map.name })}
      aria-label={t("live.mapLabel", { name: map.name })}
      onClick={() => onPlace(map)}
      className={`flex w-full min-w-0 cursor-grab items-center gap-2 rounded px-1.5 py-1 text-left text-sm hover:bg-raised ${isDragging ? "opacity-50" : ""}`}
      {...attributes}
      {...listeners}
    >
      <FontAwesomeIcon icon={faMap} fixedWidth className="shrink-0 text-xs text-muted" aria-hidden />
      <span className="min-w-0 flex-1 truncate">{map.name}</span>
      <span className="shrink-0 text-[10px] text-muted">{hint}</span>
    </button>
  );
}

/** Today's detections, newest first. A click puts that camera on the selected cell. */
export function LiveDetectionsPanel({ canEvents, onPick }: { canEvents: boolean; onPick: (cameraId: string) => void }) {
  const t = useT();
  const [page, setPage] = useState(0);
  const day = useMemo(() => localDayBounds(), []);
  const events = useInfiniteQuery({
    ...eventsQuery({ from: day.from, to: day.to, limit: PAGE }),
    enabled: canEvents,
  });
  const pages = events.data?.pages ?? [];
  const items = pages[page]?.items ?? [];
  const lastPage = Math.max(0, pages.length - 1);
  const canNext = page < lastPage || !!events.hasNextPage;
  async function goNext() {
    if (page < lastPage) {
      setPage(page + 1);
      return;
    }
    if (!events.hasNextPage || events.isFetchingNextPage) return;
    await events.fetchNextPage();
    setPage(page + 1);
  }
  if (!canEvents) return <p className="p-2 text-xs text-muted">{t("live.detectionsNeedPermission")}</p>;
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      {events.isError && <p role="alert" className="p-2 text-xs text-bad">{t("live.detectionsLoadFailed")}</p>}
      {events.isLoading && <p role="status" className="p-2 text-xs text-muted">{t("live.loadingDetections")}</p>}
      {!events.isLoading && !events.isError && !items.length && <p className="p-2 text-xs text-muted">{t("live.noDetectionsToday")}</p>}
      {items.length > 0 && (
        <ul className="min-h-0 flex-1 space-y-2 overflow-auto" aria-label={t("live.detectionsToday")}>
          {items.map((event) => (
            <li key={event.id}>
              <button
                type="button"
                onClick={() => onPick(event.camera_id)}
                className="flex w-full flex-col gap-1.5 rounded-xl border border-line bg-bg/40 p-2 text-left hover:border-accent/40"
                aria-label={t("live.detectionOf", { name: event.camera_name })}
              >
                <img
                  src={event.has_snapshot ? `/media/v1/events/${event.id}/snapshot.jpg` : `/api/v1/events/${event.id}/thumbnail`}
                  alt=""
                  className="h-20 w-full rounded-md bg-black object-contain"
                />
                <VehicleFacts
                  labels={event.labels}
                  vehicle={event.attributes?.vehicle}
                  person={event.attributes?.person}
                  vehicleJob={event.attributes?.vehicle_job}
                  personJob={event.attributes?.person_job}
                  cameraName={event.camera_name}
                  serverName={event.server_name}
                />
                <span className="text-[10px] text-muted">{fmtDateTime(event.start_time)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      <Pager page={page} canNext={canNext} pending={events.isFetchingNextPage} onPrev={() => setPage(page - 1)} onNext={() => void goNext()} />
    </div>
  );
}

/** Today's plate reads across every site. A click puts that camera on the selected cell. */
export function LiveLprPanel({ canLpr, onPick }: { canLpr: boolean; onPick: (cameraId: string) => void }) {
  const t = useT();
  const [draft, setDraft] = useState("");
  const [plate, setPlate] = useState("");
  const [page, setPage] = useState(0);
  const [pagePlate, setPagePlate] = useState(plate);
  if (pagePlate !== plate) {
    setPagePlate(plate);
    setPage(0);
  }
  const day = useMemo(() => localDayBounds(), []);
  useEffect(() => {
    const timer = window.setTimeout(() => setPlate(draft.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [draft]);
  const reads = useInfiniteQuery({
    ...platesQuery({ plate: plate || undefined, from: day.from, to: day.to, limit: PAGE }),
    enabled: canLpr,
    refetchInterval: 15_000,
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
  if (!canLpr) return <p className="p-2 text-xs text-muted">{t("maps.platesNeedPermission")}</p>;
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <input
        type="search"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        placeholder={t("maps.searchPlate")}
        aria-label={t("maps.searchPlate")}
        className="w-full rounded-lg border border-line bg-bg px-2 py-1.5 text-xs"
      />
      {reads.isError && <p role="alert" className="p-2 text-xs text-bad">{t("maps.loadFailed")}</p>}
      {reads.isLoading && <p role="status" className="p-2 text-xs text-muted">{t("maps.loadingPlates")}</p>}
      {!reads.isLoading && !reads.isError && !items.length && (
        <p className="p-2 text-xs text-muted">{plate ? t("maps.noSearchToday") : t("maps.noReadsToday")}</p>
      )}
      {items.length > 0 && (
        <ul className="min-h-0 flex-1 space-y-2 overflow-auto" aria-label={t("maps.readings")}>
          {items.map((read) => (
            <PlateReadCard key={read.id} read={read} onClick={() => onPick(read.camera_id)} />
          ))}
        </ul>
      )}
      <Pager page={page} canNext={canNext} pending={reads.isFetchingNextPage} onPrev={() => setPage(page - 1)} onNext={() => void goNext()} />
    </div>
  );
}
