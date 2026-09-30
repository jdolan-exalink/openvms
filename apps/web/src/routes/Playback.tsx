import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Download, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { camerasQuery, eventsQuery, meQuery, recordingsQuery } from "@/api/queries";
import { HlsPlayer, type HlsPlayerHandle } from "@/components/HlsPlayer";
import { RecordingTimeline } from "@/components/RecordingTimeline";
import { Button, ErrorNote, Field, PageHeader, Select, TextInput } from "@/components/ui";
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
        title="Grabaciones"
        description="La grabación se reproduce sincronizada desde el Frigate de origen; el VMS no guarda video."
      />
      <div className="grid gap-3 sm:grid-cols-3">
        <Field label={selectedCameraIds.length > 0 ? "Cámara principal" : "Cámara"}>
          <Select
            value={primaryCameraId}
            onChange={(e) => handleSelectFirstCamera(e.target.value)}
          >
            <option value="">Elegí una cámara</option>
            {cameras.data?.map((c) => (
              <option key={c.id} value={c.id}>
                {c.display_name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Día">
          <div className="flex items-center gap-1">
            <Button aria-label="Día anterior" onClick={() => setDay(day - DAY)}>
              <ChevronLeft className="size-4" />
            </Button>
            <TextInput
              type="date"
              value={toLocalInput(new Date(day * 1000)).slice(0, 10)}
              onChange={(e) => e.target.value && setDay(startOfDay(new Date(e.target.value + "T00:00").getTime() / 1000))}
            />
            <Button aria-label="Día siguiente" onClick={() => setDay(Math.min(day + DAY, startOfDay(now)))}>
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </Field>
        <Field label="Ir a">
          <TextInput
            type="datetime-local"
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
        <div className="flex flex-wrap items-center justify-between gap-2 rounded border border-line bg-surface p-2 text-xs">
          <div className="flex items-center gap-2">
            <span className="text-muted">Cámaras sincronizadas ({selectedCameraIds.length}/4):</span>
            <div className="flex flex-wrap items-center gap-1.5">
              {selectedCameraIds.map((id) => {
                const cam = cameraMap.get(id);
                const isPrimary = id === primaryCameraId;
                return (
                  <span
                    key={id}
                    className={`inline-flex items-center gap-1 rounded px-2 py-0.5 font-medium border ${
                      isPrimary
                        ? "bg-accent/15 border-accent/40 text-accent"
                        : "bg-raised border-line text-ink"
                    }`}
                  >
                    <span>{cam?.display_name ?? id}</span>
                    {selectedCameraIds.length > 1 && (
                      <button
                        type="button"
                        onClick={() => removeCamera(id)}
                        aria-label={`Quitar ${cam?.display_name ?? id}`}
                        className="rounded hover:bg-black/10 dark:hover:bg-white/10"
                      >
                        <X className="size-3" aria-hidden />
                      </button>
                    )}
                  </span>
                );
              })}
            </div>
          </div>
          {availableToAdd.length > 0 && selectedCameraIds.length < 4 && (
            <div className="flex items-center gap-2">
              <Select
                className="text-xs py-1"
                value=""
                onChange={(e) => {
                  if (e.target.value) {
                    addCamera(e.target.value);
                    e.target.value = "";
                  }
                }}
              >
                <option value="">+ Agregar cámara sincronizada</option>
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
        <p className="text-sm text-muted">Elegí una cámara para ver su línea de tiempo.</p>
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
                  className={`flex flex-col gap-2 rounded-lg border p-2 bg-surface shadow-sm ${
                    isPrimary ? "border-accent/40 ring-1 ring-accent/20" : "border-line"
                  }`}
                >
                  <div className="flex items-center justify-between gap-2 px-1">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="truncate text-xs font-semibold text-ink">
                        {cam?.display_name ?? camId}
                      </span>
                      {isPrimary ? (
                        <span className="rounded bg-accent/15 px-1.5 py-0.5 text-[10px] font-medium text-accent border border-accent/30">
                          Línea de tiempo
                        </span>
                      ) : (
                        <button
                          type="button"
                          onClick={() => setPrimary(camId)}
                          className="text-[10px] text-muted hover:text-ink underline"
                        >
                          Ver línea de tiempo
                        </button>
                      )}
                    </div>
                    {selectedCameraIds.length > 1 && (
                      <button
                        type="button"
                        aria-label={`Quitar cámara ${cam?.display_name ?? camId}`}
                        onClick={() => removeCamera(camId)}
                        className="rounded p-1 text-muted hover:bg-raised hover:text-ink"
                      >
                        <X className="size-3.5" aria-hidden />
                      </button>
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
                    ariaLabel={`Reproducción HLS de ${cam?.display_name ?? "la cámara"}`}
                    className="aspect-video w-full rounded border border-line"
                  />
                </div>
              );
            })}
          </div>

          <p className="text-sm text-muted">
            {primaryCamera?.display_name} · {fmtDateTime(new Date(position * 1000))}
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
      className="grid gap-3 rounded border border-line bg-surface p-3 sm:grid-cols-4"
      onSubmit={(e) => {
        e.preventDefault();
        exp.mutate();
      }}
    >
      <div className="sm:col-span-4 text-xs font-semibold text-muted">
        Exportar clip {cameraName ? `de ${cameraName}` : ""}
      </div>
      <Field label="Exportar desde" hint="Vacío: 30 s antes de la posición actual.">
        <TextInput type="datetime-local" step={1} value={from} onChange={(e) => setFrom(e.target.value)} />
      </Field>
      <Field label="Hasta" hint="Máximo 2 horas.">
        <TextInput type="datetime-local" step={1} value={to} onChange={(e) => setTo(e.target.value)} />
      </Field>
      <Field label="Nombre">
        <TextInput value={name} onChange={(e) => setName(e.target.value)} placeholder="Opcional" />
      </Field>
      <div className="flex flex-col justify-end gap-1">
        <Button type="submit" variant="primary" disabled={exp.isPending}>
          <Download className="size-4" aria-hidden /> Exportar
        </Button>
        {exp.data && <span className="text-xs text-ok">Exportación iniciada.</span>}
      </div>
      <div className="sm:col-span-4">
        <ErrorNote error={exp.error} />
      </div>
    </form>
  );
}
