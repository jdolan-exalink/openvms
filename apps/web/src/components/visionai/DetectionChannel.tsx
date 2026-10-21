import { useEffect, useState } from "react";
import { CarFront, ScanFace } from "lucide-react";
import { ArPlate } from "@/components/plates/ArPlate";
import { cn } from "@/lib/cn";
import { CHANNEL_HISTORY, useChannelItems, type ChannelItem, type ChannelKind } from "@/lib/detectionChannels";
import { detectionAgeSeconds, formatDetectionAge } from "@/lib/visionAi";

const TITLES: Record<ChannelKind, string> = { lpr: "LPR en vivo", faces: "Rostros en vivo" };
const EMPTY: Record<ChannelKind, string> = { lpr: "Esperando lecturas de patentes", faces: "Esperando rostros" };

function formatTime(at: string): string {
  const d = new Date(at);
  return Number.isNaN(d.getTime()) ? "--:--:--" : new Intl.DateTimeFormat("es-AR", { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(d);
}

/** "hace Ns" since the latest detection, ticking every second. */
function useAge(at: string | undefined): string | null {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const seconds = detectionAgeSeconds(at, now);
  return seconds === null ? null : formatDetectionAge(seconds);
}

/**
 * A detection channel: the latest LPR read or face detection shown large, the previous ones in a
 * strip underneath. Used as a Live grid tile and as the fullscreen totem.
 */
export function DetectionChannel({ kind, className }: { kind: ChannelKind; className?: string }) {
  const query = useChannelItems(kind);
  const items = query.data ?? [];
  const [latest, ...previous] = items;
  const age = useAge(latest?.at);
  const Icon = kind === "lpr" ? CarFront : ScanFace;

  return (
    <section
      data-testid="detection-channel"
      data-channel={kind}
      aria-label={TITLES[kind]}
      aria-live="polite"
      className={cn("flex size-full min-h-0 flex-col overflow-hidden bg-surface-dim text-ink", className)}
    >
      <header className="flex shrink-0 items-center justify-between gap-2 border-b border-line bg-surface px-3 py-2">
        <div className="flex min-w-0 items-center gap-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">
          <span aria-hidden="true" className="size-2 shrink-0 rounded-full bg-emerald-500 animate-pulse" />
          <Icon aria-hidden="true" className="size-4 shrink-0" />
          <span className="truncate">{TITLES[kind]}</span>
        </div>
        {age && <span className="shrink-0 font-mono text-xs font-semibold tabular-nums text-on-surface-variant">{age}</span>}
      </header>

      {!latest ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-muted">
          <Icon aria-hidden="true" className="size-8 opacity-40" />
          <span>{query.isError ? "No se pudieron cargar las detecciones" : EMPTY[kind]}</span>
        </div>
      ) : (
        <>
          <div data-testid="channel-latest" className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-4">
            <div className="flex min-h-0 w-full flex-1 items-center justify-center overflow-hidden rounded-m3-lg bg-video">
              <img src={latest.imageUrl} alt={`${latest.label} en ${latest.cameraName}`} className="max-h-full max-w-full object-contain" />
            </div>
            <ChannelLabel item={latest} large />
            <div className="text-center text-sm text-on-surface-variant">
              <span className="font-semibold text-ink">{latest.cameraName}</span>
              {latest.zone && <span> · {latest.zone}</span>}
              <span className="font-mono tabular-nums"> · {formatTime(latest.at)}</span>
            </div>
          </div>

          {previous.length > 0 && (
            <ol data-testid="channel-history" aria-label="Detecciones anteriores" className="flex shrink-0 gap-2 overflow-x-auto border-t border-line bg-surface p-2">
              {previous.slice(0, CHANNEL_HISTORY).map((item) => (
                <li key={item.key} data-testid="channel-history-item" className="flex w-28 shrink-0 flex-col gap-1">
                  <img src={item.imageUrl} alt="" loading="lazy" className="aspect-video w-full rounded-m3-sm bg-video object-cover" />
                  <ChannelLabel item={item} />
                  <span className="truncate font-mono text-[10px] tabular-nums text-muted">{formatTime(item.at)}</span>
                </li>
              ))}
            </ol>
          )}
        </>
      )}
    </section>
  );
}

function ChannelLabel({ item, large = false }: { item: ChannelItem; large?: boolean }) {
  if (item.kind === "lpr") return <ArPlate plate={item.label} large={large} />;
  return (
    <span
      className={cn(
        "truncate font-semibold",
        large ? "text-2xl" : "text-xs",
        item.recognized ? "text-ink" : "text-on-surface-variant italic",
      )}
    >
      {item.label}
    </span>
  );
}
