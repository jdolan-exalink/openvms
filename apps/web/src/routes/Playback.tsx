import { useT } from "@/i18n";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Download, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { camerasQuery, eventsQuery, meQuery, recordingsQuery } from "@/api/queries";
import { HlsPlayer, type HlsPlayerHandle } from "@/components/HlsPlayer";
import { RecordingTimeline } from "@/components/RecordingTimeline";
import { Button, ErrorNote, Field, IconButton, PageHeader, RemovableChip, Select, TextInput } from "@/components/ui";
import { fmtDateTime, toLocalInput } from "@/lib/format";
import { vodWindowForInstant } from "@/lib/recordings";
import { useSyncedPlayback } from "@/lib/useSyncedPlayback";
import { can } from "@/lib/perm";

const DAY = 24 * 3600;

function startOfDay(unix: number): number {
  const d = new Date(unix * 1000);
  d.setHours(0, 0, 0, 0);
  return Math.floor(d.getTime() / 1000);
}

const unixNow = () => Math.floor(Date.now() / 1000);
const INITIAL_NOW = unixNow();

/**
 * Playback (PRD §51-55, §122 M6): multi-camera synchronized playback built from Frigate's
 * recordings and the VMS event index, HLS playback from the origin Frigate, and clip export.
 */
export function Playback() {
  const t = useT();
  const search = useSearch({ from: "/app/playback" });
  const navigate = useNavigate();
  const me = useQuery(meQuery);
  const cameras = useQuery(camerasQuery({}));

  const selectedCameraIds = useMemo(() => {
    if (search.cameras && search.cameras.length > 0) {
      return search.cameras.slice(0, 4);
    }
    if (search.camera) {
      return [search.camera];
    }
    return [];
  }, [search.cameras, search.camera]);

  const primaryCameraId = useMemo(() => {
    if (search.camera && selectedCameraIds.includes(search.camera)) {
      return search.camera;
    }
    return selectedCameraIds[0] ?? "";
  }, [search.camera, selectedCameraIds]);

  const [now, setNow] = useState(INITIAL_NOW);
  const [instant, setInstant] = useState<number>(search.t ?? now - 600);
  const [day, setDay] = useState<number>(startOfDay(search.t ?? now));
  const [position, setPosition] = useState<number>(instant);

  useEffect(() => {
    const refreshId = setTimeout(() => {
      const currentNow = unixNow();
      setNow(currentNow);
      if (search.t === undefined) {
        const currentInstant = currentNow - 600;
        setInstant(currentInstant);
        setPosition(currentInstant);
        setDay(startOfDay(currentNow));
      }
    }, 0);
    const id = setInterval(() => setNow(unixNow()), 30_000);
    return () => {
      clearTimeout(refreshId);
      clearInterval(id);
    };
  }, [search.t]);

  const from = new Date(day * 1000).toISOString();
  const to = new Date((day + DAY) * 1000).toISOString();

  // Timeline spans and events follow the primary camera
  const recordings = useQuery(recordingsQuery(primaryCameraId, from, to));
  const events = useInfiniteQuery({
    ...eventsQuery({ camera_id: primaryCameraId ? [primaryCameraId] : undefined, from, to, limit: 500 }),
    enabled: !!primaryCameraId,
  });
  const dayEvents = events.data?.pages.flatMap((p) => p.items) ?? [];

  const { start: winStart, end: winEnd } = vodWindowForInstant(instant, day, now);
  const cameraMap = useMemo(() => new Map(cameras.data?.map((c) => [c.id, c])), [cameras.data]);
  const primaryCamera = cameraMap.get(primaryCameraId);

  const updateSelection = (newIds: string[], newPrimary?: string) => {
    const clamped = newIds.slice(0, 4);
    const primary = newPrimary && clamped.includes(newPrimary) ? newPrimary : (clamped[0] ?? undefined);
    void navigate({
      to: "/playback",
      search: {
        camera: primary,
        cameras: clamped.length > 0 ? clamped : undefined,
        t: instant,
      },
    });
  };

  const handleSelectFirstCamera = (id: string) => {
    if (!id) {
      updateSelection([]);
      return;
    }
    updateSelection([id], id);
  };

  const addCamera = (id: string) => {
    if (!id || selectedCameraIds.includes(id) || selectedCameraIds.length >= 4) return;
    updateSelection([...selectedCameraIds, id], primaryCameraId || id);
  };

  const removeCamera = (id: string) => {
    const next = selectedCameraIds.filter((c) => c !== id);
    updateSelection(next, id === primaryCameraId ? next[0] : primaryCameraId);
  };

  const setPrimary = (id: string) => {
    if (selectedCameraIds.includes(id)) {
      updateSelection(selectedCameraIds, id);
    }
  };

  // Drift correction: the primary player is the master clock for the other cameras.
  const players = useRef(new Map<string, HlsPlayerHandle | null>());
  useSyncedPlayback(primaryCameraId, selectedCameraIds, (id) => players.current.get(id)?.video);

  const jump = (t: number) => {
    setInstant(t);
    setPosition(t);
  };

  // Available cameras that can still be added (up to max 4)
  const availableToAdd = useMemo(() => {
    if (!cameras.data || selectedCameraIds.length >= 4) return [];
    return cameras.data.filter((c) => !selectedCameraIds.includes(c.id));
  }, [cameras.data, selectedCameraIds]);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4">
      <PageHeader
        title={t("nav.recordings")}
        description={t("settings.playback")}
      />
      <div className="grid gap-3 rounded-m3-xl bg-surface-1 p-4 md:grid-cols-3">
        <Field label={selectedCameraIds.length > 0 ? t("live.primaryCamera") : t("common.camera")}>
          <Select
            value={primaryCameraId}
            onChange={(e) => handleSelectFirstCamera(e.target.value)}
          >
            <option value="">{t("live.chooseCamera")}</option>
            {cameras.data?.map((c) => (
              <option key={c.id} value={c.id}>
                {c.display_name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t("live.day")}>
          <div className="flex items-center gap-2">
            <IconButton variant="tonal" icon={ChevronLeft} aria-label={t("live.previousDay")} onClick={() => setDay(day - DAY)} />
            <TextInput
              type="date"
              className="font-mono"
              value={toLocalInput(new Date(day * 1000)).slice(0, 10)}
              onChange={(e) => e.target.value && setDay(startOfDay(new Date(e.target.value + "T00:00").getTime() / 1000))}
            />
            <IconButton variant="tonal" icon={ChevronRight} aria-label={t("live.nextDay")} onClick={() => setDay(Math.min(day + DAY, startOfDay(now)))} />
          </div>
        </Field>
        <Field label={t("live.goTo")}>
          <TextInput
            type="datetime-local"
            className="font-mono"
            value={toLocalInput(new Date(instant * 1000))}
            onChange={(e) => {
              const t = Math.floor(new Date(e.target.value).getTime() / 1000);
              if (!Number.isNaN(t)) {
                setDay(startOfDay(t));
                jump(t);
              }
            }}
          />
        </Field>
      </div>

      {/* Multi-camera toolbar */}
      {selectedCameraIds.length > 0 && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-m3-xl bg-surface-1 p-3 text-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-muted">{t("live.syncedCameras", { count: selectedCameraIds.length })}</span>
            <div className="flex flex-wrap items-center gap-1.5">
              {selectedCameraIds.map((id) => {
                const cam = cameraMap.get(id);
                const isPrimary = id === primaryCameraId;
                return (
                  <RemovableChip
                    key={id}
                    tone={isPrimary ? "primary" : "secondary"}
                    label={cam?.display_name ?? id}
                    removeLabel={t("live.removeNamed", { name: cam?.display_name ?? id })}
                    onRemove={selectedCameraIds.length > 1 ? () => removeCamera(id) : undefined}
                  />
                );
              })}
            </div>
          </div>
          {availableToAdd.length > 0 && selectedCameraIds.length < 4 && (
            <div className="flex items-center gap-2">
              <Select
                value=""
                onChange={(e) => {
                  if (e.target.value) {
                    addCamera(e.target.value);
                    e.target.value = "";
                  }
                }}
              >
                <option value="">{t("live.addSyncedCamera")}</option>
                {availableToAdd.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.display_name}
                  </option>
                ))}
              </Select>
            </div>
          )}
        </div>
      )}

      {selectedCameraIds.length === 0 && (
        <p className="text-sm text-muted">{t("live.chooseCameraTimeline")}</p>
      )}

      {selectedCameraIds.length > 0 && (
        <>
          <RecordingTimeline
            day={day}
            spans={recordings.data ?? []}
            events={dayEvents}
            position={position}
            onSeek={(t) => jump(t)}
          />
          <ErrorNote error={recordings.error} />

          {/* Synchronized video grid */}
          <div
            className={`grid gap-4 ${
              selectedCameraIds.length === 1
                ? "grid-cols-1"
                : selectedCameraIds.length === 2
                  ? "grid-cols-1 md:grid-cols-2"
                  : "grid-cols-1 sm:grid-cols-2"
            }`}
          >
            {selectedCameraIds.map((camId) => {
              const cam = cameraMap.get(camId);
              const isPrimary = camId === primaryCameraId;
              return (
                <div
                  key={camId}
                  className={`flex flex-col gap-2 rounded-m3-xl bg-surface-1 p-2 ${
                    isPrimary ? "ring-2 ring-primary/50" : ""
                  }`}
                >
                  <div className="flex items-center justify-between gap-2 px-1">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="truncate text-sm font-bold text-on-surface">
                        {cam?.display_name ?? camId}
                      </span>
                      {isPrimary ? (
                        <span className="rounded-full bg-primary-container px-2.5 py-0.5 text-xs font-medium text-on-primary-container">
                          {t("live.timeline")}
                        </span>
                      ) : (
                        <button
                          type="button"
                          onClick={() => setPrimary(camId)}
                          className="inline-flex min-h-9 items-center rounded-full px-3 text-xs font-medium text-primary hover:bg-primary/10 focus-visible:outline-2 focus-visible:outline-primary"
                        >
                          {t("live.viewTimeline")}
                        </button>
                      )}
                    </div>
                    {selectedCameraIds.length > 1 && (
                      <IconButton icon={X} aria-label={t("live.removeCamera", { name: cam?.display_name ?? camId })} onClick={() => removeCamera(camId)} />
                    )}
                  </div>

                  <HlsPlayer
                    ref={(h) => {
                      if (h) players.current.set(camId, h);
                      else players.current.delete(camId);
                    }}
                    key={`${camId}-${winStart}`}
                    cameraId={camId}
                    start={winStart}
                    end={winEnd}
                    startOffset={instant - winStart}
                    onTime={isPrimary ? setPosition : undefined}
                    ariaLabel={t("live.hlsPlayback", { name: cam?.display_name ?? t("live.theCamera") })}
                    className="aspect-video w-full overflow-hidden rounded-m3-xl bg-video"
                  />
                </div>
              );
            })}
          </div>

          <p className="text-sm text-muted">
            {primaryCamera?.display_name} · <span className="font-mono">{fmtDateTime(new Date(position * 1000))}</span>
          </p>

          {can(me.data, "exports.create") && (
            <ExportForm
              cameraId={primaryCameraId}
              position={position}
              cameraName={primaryCamera?.display_name}
            />
          )}
        </>
      )}
    </div>
  );
}

function ExportForm({
  cameraId,
  position,
  cameraName,
}: {
  cameraId: string;
  position: number;
  cameraName?: string;
}) {
  const t = useT();
  const qc = useQueryClient();
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [name, setName] = useState("");
  const exp = useMutation({
    mutationFn: async () => {
      const start = new Date(from || toLocalInput(new Date((position - 30) * 1000)));
      const end = new Date(to || toLocalInput(new Date((position + 30) * 1000)));
      return unwrap(
        await api.POST("/api/v1/exports", {
          body: {
            camera_id: cameraId,
            start_time: start.toISOString(),
            end_time: end.toISOString(),
            name: name.trim() || undefined,
          },
        }),
      );
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["exports"] }),
  });

  return (
    <form
      className="grid gap-3 rounded-m3-xl bg-surface-1 p-4 sm:grid-cols-2 lg:grid-cols-4"
      onSubmit={(e) => {
        e.preventDefault();
        exp.mutate();
      }}
    >
      <div className="text-base font-bold sm:col-span-2 lg:col-span-4">
        {cameraName ? t("live.exportClipOf", { name: cameraName }) : t("events.exportClip")}
      </div>
      <Field label={t("live.exportFrom")} hint={t("live.exportFromHint")}>
        <TextInput type="datetime-local" step={1} value={from} onChange={(e) => setFrom(e.target.value)} />
      </Field>
      <Field label={t("common.to")} hint={t("live.untilHint")}>
        <TextInput type="datetime-local" step={1} value={to} onChange={(e) => setTo(e.target.value)} />
      </Field>
      <Field label={t("live.clipName")}>
        <TextInput value={name} onChange={(e) => setName(e.target.value)} placeholder={t("live.optional")} />
      </Field>
      <div className="flex flex-col justify-end gap-1">
        <Button type="submit" variant="filled" disabled={exp.isPending}>
          <Download className="size-4" aria-hidden /> {t("live.export")}
        </Button>
        {exp.data && <span className="text-xs text-ok">{t("live.exportStartedShort")}</span>}
      </div>
      <div className="sm:col-span-2 lg:col-span-4">
        <ErrorNote error={exp.error} />
      </div>
    </form>
  );
}
