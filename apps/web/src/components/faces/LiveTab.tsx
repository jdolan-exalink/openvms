import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { faceRecognitionsQuery, faceReviewInboxQuery } from "@/api/queries";
import { emptyScope, type Scope, ScopeFilters } from "@/components/faces/ScopeFilters";
import { Button, Empty, ErrorNote, LinkButton, Pill } from "@/components/ui";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import { type FeedFilter, type FeedItem, filterFeed, mergeFeed } from "@/lib/faceFeed";
import { fmtDateTime } from "@/lib/format";

const LIVE_REFRESH_MS = 5_000;
const filters: { id: FeedFilter; label: "faces.liveFilterAll" | "faces.liveFilterKnown" | "faces.liveFilterUnknown" }[] = [
  { id: "all", label: "faces.liveFilterAll" },
  { id: "known", label: "faces.liveFilterKnown" },
  { id: "unknown", label: "faces.liveFilterUnknown" },
];

function FeedRow({ item }: { item: FeedItem }) {
  const t = useT();
  const known = item.kind === "known";
  return (
    <li className={cn("flex gap-3 rounded-m3-xl border-l-4 bg-surface-1 p-3", known ? "border-ok" : "border-warn")}>
      {item.thumb ? (
        <img src={item.thumb} alt="" loading="lazy" className="size-16 shrink-0 rounded-m3-lg bg-surface-2 object-cover" />
      ) : (
        <div className="size-16 shrink-0 rounded-m3-lg bg-surface-2" aria-hidden />
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-1 text-sm">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate font-bold">{known ? item.name : t("faces.liveUnknownGroup")}</span>
          <Pill tone={known ? "ok" : "warn"} size="sm">{known ? t("faces.liveKnown") : t("faces.liveUnknown")}</Pill>
        </div>
        <span className="truncate text-xs text-muted">{item.place} · {fmtDateTime(new Date(item.at))}</span>
        {item.playback && (
          <LinkButton to="/playback" search={{ camera: item.playback.camera, t: item.playback.t }} variant="text" size="sm" className="w-fit">
            {t("faces.viewPlayback")}
          </LinkButton>
        )}
      </div>
    </li>
  );
}

/**
 * LiveTab shows faces as they arrive, recognized and unknown on one timeline. It polls the same two
 * endpoints the other tabs use; Pause freezes the list so a row can be read without it moving.
 */
export function LiveTab() {
  const t = useT();
  const [scope, setScope] = useState<Scope>(emptyScope);
  const [filter, setFilter] = useState<FeedFilter>("all");
  const [frozen, setFrozen] = useState<FeedItem[] | null>(null);
  const site = scope.site ? [scope.site] : undefined;
  const server = scope.server ? [scope.server] : undefined;
  const recognitions = useInfiniteQuery({ ...faceRecognitionsQuery({ site_id: site, server_id: server, limit: 40 }), refetchInterval: LIVE_REFRESH_MS });
  const sightings = useQuery({ ...faceReviewInboxQuery({ site_id: site, server_id: server }), refetchInterval: LIVE_REFRESH_MS });
  const live = useMemo(
    () => mergeFeed(recognitions.data?.pages.flatMap((p) => p.items) ?? [], sightings.data?.items ?? []),
    [recognitions.data, sightings.data],
  );
  const shown = filterFeed(frozen ?? live, filter);

  return (
    <div className="flex flex-col gap-4">
      <ScopeFilters value={scope} onChange={setScope} />
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-base font-medium">{t("faces.liveTitle")}</h2>
        <div className="flex flex-wrap items-center gap-1" role="group" aria-label={t("faces.liveTitle")}>
          {filters.map((item) => (
            <Button key={item.id} variant={filter === item.id ? "tonal" : "text"} aria-pressed={filter === item.id} onClick={() => setFilter(item.id)}>
              {t(item.label)}
            </Button>
          ))}
          <Button variant="outlined" aria-pressed={frozen !== null} onClick={() => setFrozen(frozen ? null : live)}>
            {frozen ? t("faces.liveResume") : t("faces.livePause")}
          </Button>
        </div>
      </div>
      <ErrorNote error={recognitions.error ?? sightings.error} />
      {shown.length === 0 && !recognitions.isPending && <Empty>{t("faces.liveEmpty")}</Empty>}
      <ul className="flex flex-col gap-2">
        {shown.map((item) => (
          <FeedRow key={item.key} item={item} />
        ))}
      </ul>
    </div>
  );
}
