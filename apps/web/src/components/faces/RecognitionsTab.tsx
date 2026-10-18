import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { History } from "lucide-react";
import { useState } from "react";
import { camerasQuery, faceRecognitionsQuery, facePersonsQuery } from "@/api/queries";
import { Button, Empty, ErrorNote, Field, LinkButton, Select, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { fmtDateTime, fromLocalInput } from "@/lib/format";

/** RecognitionsTab is the timeline of recognized people, newest first, with person/camera/date filters. */
export function RecognitionsTab({ tenantId }: { tenantId?: string }) {
  const t = useT();
  const persons = useQuery(facePersonsQuery(tenantId));
  const cameras = useQuery(camerasQuery({}));
  const [person, setPerson] = useState("");
  const [camera, setCamera] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const list = useInfiniteQuery(
    faceRecognitionsQuery({
      person_id: person ? [person] : undefined,
      camera_id: camera ? [camera] : undefined,
      from: fromLocalInput(from),
      to: fromLocalInput(to),
      limit: 24,
      tenant_id: tenantId,
    }),
  );
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-3 rounded-m3-xl bg-surface-1 p-4 sm:grid-cols-2 lg:grid-cols-4">
        <Field label={t("faces.person")}>
          <Select value={person} onChange={(e) => setPerson(e.target.value)}>
            <option value="">{t("faces.allPeople")}</option>
            {persons.data?.map((p) => (
              <option key={p.id} value={p.id}>{p.display_name}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.camera")}>
          <Select value={camera} onChange={(e) => setCamera(e.target.value)}>
            <option value="">{t("common.all")}</option>
            {cameras.data?.map((c) => (
              <option key={c.id} value={c.id}>{c.display_name}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.from")}>
          <TextInput type="datetime-local" className="font-mono" value={from} onChange={(e) => setFrom(e.target.value)} />
        </Field>
        <Field label={t("common.to")}>
          <TextInput type="datetime-local" className="font-mono" value={to} onChange={(e) => setTo(e.target.value)} />
        </Field>
      </div>
      <ErrorNote error={list.error} />
      {list.isSuccess && items.length === 0 && <Empty icon={History}>{t("faces.recEmpty")}</Empty>}
      {items.length > 0 && (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-label={t("faces.recList")}>
          {items.map((r) => (
            <li key={`${r.event_id}/${r.person_id}`} className="flex gap-3 rounded-m3-xl bg-surface-1 p-3">
              {r.has_thumbnail ? (
                <img src={`/api/v1/events/${r.event_id}/thumbnail`} alt={t("faces.recThumbAlt", { camera: r.camera_name })} loading="lazy" className="size-24 shrink-0 rounded-m3-lg bg-surface-2 object-cover" />
              ) : (
                <div className="size-24 shrink-0 rounded-m3-lg bg-surface-2" aria-hidden />
              )}
              <div className="flex min-w-0 flex-col gap-1 text-sm">
                <span className="truncate font-bold">{r.person_name}</span>
                <span className="truncate">{r.camera_name}</span>
                <span className="truncate text-xs text-muted">{r.site_name} · {fmtDateTime(r.start_time)}</span>
                <LinkButton
                  to="/playback"
                  search={{ camera: r.camera_id, t: Math.floor(new Date(r.start_time).getTime() / 1000) }}
                  variant="text"
                  size="sm"
                  className="w-fit"
                >
                  {t("faces.viewPlayback")}
                </LinkButton>
              </div>
            </li>
          ))}
        </ul>
      )}
      {list.hasNextPage && (
        <Button variant="tonal" className="self-center" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
          {list.isFetchingNextPage ? t("common.loading") : t("faces.loadMore")}
        </Button>
      )}
    </div>
  );
}
