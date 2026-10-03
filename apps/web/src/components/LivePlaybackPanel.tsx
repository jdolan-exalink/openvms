import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useT } from "@/i18n";
import { eventsQuery, recordingsQuery } from "@/api/queries";
import { HlsPlayer } from "@/components/HlsPlayer";
import { RecordingTimeline } from "@/components/RecordingTimeline";
import { Button, ErrorNote, Field, TextInput } from "@/components/ui";
import { fmtDateTime, toLocalInput } from "@/lib/format";
import { vodWindowForInstant } from "@/lib/recordings";

const DAY = 24 * 3600;
const unixNow = () => Math.floor(Date.now() / 1000);

function startOfDay(unix: number): number {
  const date = new Date(unix * 1000);
  date.setHours(0, 0, 0, 0);
  return Math.floor(date.getTime() / 1000);
}

export function LivePlaybackPanel({
  cameraId,
  cameraName,
  onClose,
}: {
  cameraId: string;
  cameraName: string;
  onClose: () => void;
}) {
  const t = useT();
  const [now, setNow] = useState(unixNow);
  const [instant, setInstant] = useState(now - 600);
  const [day, setDay] = useState(startOfDay(now));
  const [position, setPosition] = useState(instant);
  useEffect(() => {
    const timer = setInterval(() => setNow(unixNow()), 30_000);
    return () => clearInterval(timer);
  }, []);

  const from = new Date(day * 1000).toISOString();
  const to = new Date((day + DAY) * 1000).toISOString();
  const recordings = useQuery(recordingsQuery(cameraId, from, to));
  const events = useInfiniteQuery({
    ...eventsQuery({ camera_id: [cameraId], from, to, limit: 500 }),
    enabled: !!cameraId,
  });
  const dayEvents = events.data?.pages.flatMap((page) => page.items) ?? [];
  const { start: winStart, end: winEnd } = vodWindowForInstant(instant, day, now);
  const jump = (time: number) => {
    setInstant(time);
    setPosition(time);
  };

  return (
    <section aria-label={t("live.recordingsOf", { name: cameraName })} className="flex flex-col gap-3 rounded-xl border border-line bg-surface p-3">
      <header className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="truncate text-sm font-semibold">{t("live.recordingsHeading", { name: cameraName })}</h2>
          <p className="text-xs text-muted">{t("live.timelineAndHls")}</p>
        </div>
        <Button aria-label={t("live.closeRecordings")} onClick={onClose}><X className="size-4" aria-hidden /></Button>
      </header>
      <div className="grid gap-2 sm:grid-cols-2">
        <Field label={t("live.day")}>
          <div className="flex items-center gap-1">
            <Button aria-label={t("live.previousDay")} onClick={() => setDay(day - DAY)}><ChevronLeft className="size-4" /></Button>
            <TextInput
              type="date"
              value={toLocalInput(new Date(day * 1000)).slice(0, 10)}
              onChange={(event) => event.target.value && setDay(startOfDay(new Date(`${event.target.value}T00:00`).getTime() / 1000))}
            />
            <Button aria-label={t("live.nextDay")} onClick={() => setDay(Math.min(day + DAY, startOfDay(now)))}><ChevronRight className="size-4" /></Button>
          </div>
        </Field>
        <Field label={t("live.goTo")}>
          <TextInput
            type="datetime-local"
            value={toLocalInput(new Date(instant * 1000))}
            onChange={(event) => {
              const time = Math.floor(new Date(event.target.value).getTime() / 1000);
              if (!Number.isNaN(time)) {
                setDay(startOfDay(time));
                jump(time);
              }
            }}
          />
        </Field>
      </div>
      <RecordingTimeline day={day} spans={recordings.data ?? []} events={dayEvents} position={position} onSeek={jump} />
      <ErrorNote error={recordings.error ?? events.error} />
      <HlsPlayer
        key={`${cameraId}-${winStart}`}
        cameraId={cameraId}
        start={winStart}
        end={winEnd}
        startOffset={instant - winStart}
        onTime={setPosition}
        ariaLabel={t("live.hlsPlayback", { name: cameraName })}
        className="aspect-video w-full rounded border border-line"
      />
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted">
        <span>{fmtDateTime(new Date(position * 1000))}</span>
        <Link to="/playback" search={{ camera: cameraId }} className="text-accent underline">
          {t("live.openRecordingsPageOf", { name: cameraName })}
        </Link>
      </div>
    </section>
  );
}
