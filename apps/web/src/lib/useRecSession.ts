import { useNavigate, useSearch } from "@tanstack/react-router";
import { useEffect, useMemo, useRef, useState } from "react";
import { assignRecPlayers, parseRecSearch, pickMaster, REC_ENTRY_OFFSET_S, recSearch } from "@/lib/liveRec";
import { useRecData } from "@/lib/useRecData";
import { useRecPlayback } from "@/lib/useRecPlayback";
import { useSyncedPlayback } from "@/lib/useSyncedPlayback";

/** How often the shared REC time is written to the URL while playing. */
const URL_SYNC_MS = 15_000;

const unixNow = () => Math.floor(Date.now() / 1000);

type CameraInfo = { display_name: string; status?: string };

/**
 * useRecSession is the whole Live GRABADO session shared by the desktop grid and the phone
 * pages: the URL contract (`?mode=rec&t=<ISO>`), the synchronized transport, the coverage and
 * event data of the cameras on screen, which of them get a recorded player, the master that
 * drives drift correction, the entry jump to the last recording and the `?t=` write-back.
 * `cameraIds` are the cameras on screen; `focusedId` narrows playback to one camera.
 */
export function useRecSession({
  canRec,
  cameraIds,
  focusedId,
  selectedId,
  cameraById,
}: {
  canRec: boolean;
  cameraIds: string[];
  focusedId?: string;
  selectedId?: string;
  cameraById: ReadonlyMap<string, CameraInfo>;
}) {
  const search = useSearch({ strict: false }) as Record<string, unknown>;
  const navigate = useNavigate();
  const { rec: urlRec, t: urlT } = parseRecSearch(search);
  const rec = urlRec && canRec;
  const [now, setNow] = useState(unixNow);
  useEffect(() => {
    if (!rec) return;
    const id = setInterval(() => setNow(unixNow()), 30_000);
    return () => clearInterval(id);
  }, [rec]);

  const transport = useRecPlayback({ active: rec, seedT: urlT ?? now - REC_ENTRY_OFFSET_S, now });
  const recData = useRecData(cameraIds, transport.day, rec);
  const denied = useMemo(() => new Set(recData.denied), [recData.denied]);
  const { players: recPlayers, limited: recLimited } = useMemo(
    () => assignRecPlayers(focusedId ? [focusedId] : cameraIds, (id) => !denied.has(id)),
    [focusedId, cameraIds, denied],
  );
  const hasCoverage = (id: string) => (recData.spans[id]?.length ?? 0) > 0;
  const syncIds = useMemo(() => recPlayers.filter((id) => !recData.loaded.includes(id) || (recData.spans[id]?.length ?? 0) > 0), [recPlayers, recData.loaded, recData.spans]);
  const masterId = pickMaster(syncIds, selectedId, hasCoverage);
  useSyncedPlayback(rec ? masterId : "", syncIds, (id) => transport.players.current.get(id)?.video);
  const timelineCameras = useMemo(
    () => cameraIds.filter((id) => !denied.has(id)).map((id) => ({ id, name: cameraById.get(id)?.display_name ?? id, spans: recData.spans[id] ?? [], live: cameraById.get(id)?.status === "online" })),
    [cameraIds, denied, cameraById, recData.spans],
  );

  // Entering GRABADO starts five minutes before now and plays. If the cameras stopped earlier, it starts at the end of the last recording.
  const entryPending = useRef(false);
  const setMode = (next: "live" | "rec") => {
    entryPending.current = next === "rec";
    void navigate({ to: ".", search: ((prev: Record<string, unknown>) => ({ ...prev, ...recSearch(next === "rec", unixNow() - REC_ENTRY_OFFSET_S) })) as never });
  };
  const { seek } = transport;
  useEffect(() => {
    if (!rec) entryPending.current = false;
  }, [rec]);
  useEffect(() => {
    if (!rec || !entryPending.current || recData.loaded.length < cameraIds.length - recData.denied.length || recData.loaded.length === 0) return;
    entryPending.current = false;
    const ends = recData.loaded.flatMap((id) => (recData.spans[id] ?? []).map((s) => s.end));
    const latest = ends.length ? Math.max(...ends) : undefined;
    if (latest !== undefined && latest < now - REC_ENTRY_OFFSET_S - 60) seek(latest - 5);
  }, [rec, recData, cameraIds.length, now, seek]);

  // Keep ?t= in step with the shared clock so reload and copied links land where the user is:
  // every URL_SYNC_MS while playing, and shortly after the last seek or pause.
  const { win, playing, getPosition, subscribePosition } = transport;
  useEffect(() => {
    if (!rec) return;
    const write = () =>
      void navigate({ to: ".", replace: true, search: ((prev: Record<string, unknown>) => ({ ...prev, ...recSearch(true, Math.floor(getPosition())) })) as never });
    let debounce: ReturnType<typeof setTimeout> | undefined;
    const unsubscribe = subscribePosition(() => {
      clearTimeout(debounce);
      debounce = setTimeout(write, 1500);
    });
    const interval = playing ? setInterval(write, URL_SYNC_MS) : undefined;
    return () => {
      unsubscribe();
      clearTimeout(debounce);
      clearInterval(interval);
    };
  }, [rec, win, playing, getPosition, subscribePosition, navigate]);

  return { rec, canRec, now, transport, recData, denied, recPlayers, recLimited, hasCoverage, masterId, timelineCameras, setMode };
}
