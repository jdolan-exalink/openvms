import { useMemo } from "react";
import { fmtTime, labelName } from "@/lib/format";

const DAY = 24 * 3600;

export function RecordingTimeline({
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
  onSeek: (time: number) => void;
}) {
  const pct = (time: number) => `${Math.min(100, Math.max(0, ((time - day) / DAY) * 100))}%`;
  const hours = useMemo(() => Array.from({ length: 25 }, (_, hour) => hour), []);
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
        onKeyDown={(event) => {
          if (event.key === "ArrowRight") onSeek(position + 60);
          if (event.key === "ArrowLeft") onSeek(position - 60);
        }}
        onClick={(event) => {
          const bounds = event.currentTarget.getBoundingClientRect();
          onSeek(Math.floor(day + ((event.clientX - bounds.left) / bounds.width) * DAY));
        }}
      >
        {spans.map((span) => {
          const start = new Date(span.start_time).getTime() / 1000;
          const end = new Date(span.end_time).getTime() / 1000;
          return <div key={span.start_time} className="absolute inset-y-0 bg-accent/30" style={{ left: pct(start), width: `calc(${pct(end)} - ${pct(start)})` }} />;
        })}
        {events.map((item) => {
          const time = new Date(item.start_time).getTime() / 1000;
          return (
            <div
              key={item.id}
              title={`${item.labels.map(labelName).join(", ")} · ${fmtTime(item.start_time)}`}
              className={item.severity === "alert" ? "absolute top-0 h-3 w-0.5 bg-bad" : "absolute top-0 h-2 w-0.5 bg-warn"}
              style={{ left: pct(time) }}
            />
          );
        })}
        <div className="absolute inset-y-0 w-0.5 bg-ink" style={{ left: pct(position) }} />
      </div>
      <div className="relative h-4 font-mono text-[10px] text-muted">
        {hours
          .filter((hour) => hour % 3 === 0)
          .map((hour) => (
            <span key={hour} className="absolute -translate-x-1/2" style={{ left: `${(hour / 24) * 100}%` }}>
              {String(hour).padStart(2, "0")}
            </span>
          ))}
      </div>
    </div>
  );
}
