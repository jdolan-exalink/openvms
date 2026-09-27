import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Download } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api, unwrap } from "@/api/client";
import { camerasQuery, eventsQuery, meQuery, recordingsQuery } from "@/api/queries";
import { HlsPlayer } from "@/components/HlsPlayer";
import { Button, ErrorNote, Field, PageHeader, Select, TextInput } from "@/components/ui";
import { fmtDateTime, fmtTime, labelName, toLocalInput } from "@/lib/format";
import { can } from "@/lib/perm";

const DAY = 24 * 3600;
// Playback loads one hour around the chosen instant; the player scrubs inside it.
const WINDOW = 3600;

function startOfDay(unix: number): number {
  const d = new Date(unix * 1000);
  d.setHours(0, 0, 0, 0);
  return Math.floor(d.getTime() / 1000);
}

const unixNow = () => Math.floor(Date.now() / 1000);

/**
 * Playback (PRD §51-55): a day timeline of one camera built from its Frigate's recordings
 * and the VMS event index, HLS playback from the origin Frigate, and clip export.
 */
export function Playback() {
  const search = useSearch({ from: "/app/playback" });
  const navigate = useNavigate();
  const me = useQuery(meQuery);
  const cameras = useQuery(camerasQuery({}));
  const cameraId = search.camera ?? "";
  const [now, setNow] = useState(unixNow);
  useEffect(() => {
    const id = setInterval(() => setNow(unixNow()), 30_000);
    return () => clearInterval(id);
  }, []);
  const [instant, setInstant] = useState<number>(search.t ?? now - 600);
  const [day, setDay] = useState<number>(startOfDay(search.t ?? now));
  const [position, setPosition] = useState<number>(instant);

  const from = new Date(day * 1000).toISOString();
  const to = new Date((day + DAY) * 1000).toISOString();
  const recordings = useQuery(recordingsQuery(cameraId, from, to));
  const events = useInfiniteQuery({
    ...eventsQuery({ camera_id: cameraId ? [cameraId] : undefined, from, to, limit: 500 }),
    enabled: !!cameraId,
  });
  const dayEvents = events.data?.pages.flatMap((p) => p.items) ?? [];

  const winStart = Math.max(instant - 60, day);
  const winEnd = Math.min(winStart + WINDOW, now);
  const camera = cameras.data?.find((c) => c.id === cameraId);

  const pick = (id: string) => void navigate({ to: "/playback", search: { camera: id || undefined, t: instant } });
  const jump = (t: number) => {
    setInstant(t);
    setPosition(t);
  };

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4">
      <PageHeader title="Grabaciones" description="La grabación se reproduce desde el Frigate de origen; el VMS no guarda video." />
      <div className="grid gap-3 sm:grid-cols-3">
        <Field label="Cámara">
          <Select value={cameraId} onChange={(e) => pick(e.target.value)}>
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

      {!cameraId && <p className="text-sm text-muted">Elegí una cámara para ver su línea de tiempo.</p>}
      {cameraId && (
        <>
          <Timeline
            day={day}
            spans={recordings.data ?? []}
            events={dayEvents}
            position={position}
            onSeek={(t) => jump(t)}
          />
          <ErrorNote error={recordings.error} />
          <HlsPlayer
            key={`${cameraId}-${winStart}`}
            cameraId={cameraId}
            start={winStart}
            end={winEnd}
            startOffset={instant - winStart}
            onTime={setPosition}
            className="aspect-video w-full rounded border border-line"
          />
          <p className="text-sm text-muted">
            {camera?.display_name} · {fmtDateTime(new Date(position * 1000))}
          </p>
          {can(me.data, "exports.create") && <ExportForm cameraId={cameraId} position={position} />}
        </>
      )}
    </div>
  );
}

function Timeline({
  day,
  spans,
  events,
  position,
  onSeek,
}: {
  day: number;
  spans: { start_time: string; end_time: string }[];
  events: { id: string; start_time: string; severity: string; labels: string[] }[];
  position: number;
  onSeek: (t: number) => void;
}) {
  const pct = (t: number) => `${Math.min(100, Math.max(0, ((t - day) / DAY) * 100))}%`;
  const hours = useMemo(() => Array.from({ length: 25 }, (_, h) => h), []);
  return (
    <div className="flex flex-col gap-1">
      <div
        className="relative h-12 cursor-pointer overflow-hidden rounded border border-line bg-raised"
        role="slider"
        aria-label="Línea de tiempo del día"
        aria-valuemin={day}
        aria-valuemax={day + DAY}
        aria-valuenow={position}
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "ArrowRight") onSeek(position + 60);
          if (e.key === "ArrowLeft") onSeek(position - 60);
        }}
        onClick={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          onSeek(Math.floor(day + ((e.clientX - r.left) / r.width) * DAY));
        }}
      >
        {spans.map((s) => {
          const a = new Date(s.start_time).getTime() / 1000;
          const b = new Date(s.end_time).getTime() / 1000;
          return <div key={s.start_time} className="absolute inset-y-0 bg-accent/30" style={{ left: pct(a), width: `calc(${pct(b)} - ${pct(a)})` }} />;
        })}
        {events.map((ev) => {
          const t = new Date(ev.start_time).getTime() / 1000;
          return (
            <div
              key={ev.id}
              title={`${ev.labels.map(labelName).join(", ")} · ${fmtTime(ev.start_time)}`}
              className={ev.severity === "alert" ? "absolute top-0 h-3 w-0.5 bg-bad" : "absolute top-0 h-2 w-0.5 bg-warn"}
              style={{ left: pct(t) }}
            />
          );
        })}
        <div className="absolute inset-y-0 w-0.5 bg-ink" style={{ left: pct(position) }} />
      </div>
      <div className="relative h-4 font-mono text-[10px] text-muted">
        {hours
          .filter((h) => h % 3 === 0)
          .map((h) => (
            <span key={h} className="absolute -translate-x-1/2" style={{ left: `${(h / 24) * 100}%` }}>
              {String(h).padStart(2, "0")}
            </span>
          ))}
      </div>
    </div>
  );
}

function ExportForm({ cameraId, position }: { cameraId: string; position: number }) {
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
          body: { camera_id: cameraId, start_time: start.toISOString(), end_time: end.toISOString(), name: name.trim() || undefined },
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
