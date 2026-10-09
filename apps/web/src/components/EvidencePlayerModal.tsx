import {
  Camera,
  Check,
  CheckCircle2,
  Clock,
  Copy,
  Download,
  FastForward,
  FileCode2,
  Layers,
  Maximize2,
  Minimize2,
  Pause,
  Play,
  Rewind,
  RotateCcw,
  ShieldCheck,
  SkipBack,
  SkipForward,
  Volume2,
  VolumeX,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { cameraFrigateDocQuery } from "@/api/queries";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, LinkButton } from "@/components/ui";
import { fmtBytes, fmtDateTime, fmtTime, labelName } from "@/lib/format";
import { positionAt, trackWindow, trailUntil, zoneEntries, type TrackPoint, type ZoneEntry } from "@/lib/objectTracks";
import { useSyncedPlayback } from "@/lib/useSyncedPlayback";
import { parseCoordinates, pointInPolygon, type Point } from "@/lib/zoneGeometry";

export interface EvidencePlayerCameraItem {
  id: string;
  camera_id: string;
  camera_name: string;
  server_name?: string;
  sha256_hash?: string;
  total_bytes: number;
  status: string;
  video_url?: string;
}

export interface EvidencePlayerJob {
  id: string;
  name: string;
  start_time: string;
  end_time: string;
  camera_count: number;
  total_bytes: number;
  manifest?: Record<string, any>;
  items?: EvidencePlayerCameraItem[];
  download_url?: string;
  protected?: boolean;
  expires_at?: string | null;
}

interface EvidencePlayerModalProps {
  job: EvidencePlayerJob;
  onClose: () => void;
  isPublic?: boolean;
  shareToken?: string;
  password?: string;
}

interface ForensicEvent {
  id: string;
  camera_id: string;
  start_time: string;
  end_time?: string | null;
  labels: string[];
  zones: string[];
  plates?: string[];
  severity?: string;
  /** Real Frigate trajectories; absent on events exported before tracks were recorded. */
  tracks?: ObjectTrack[];
}

type ObjectTrack = Schemas["ObjectTrack"];

interface VisibleTrack {
  key: string;
  ev: ForensicEvent;
  track: ObjectTrack;
  entries: ZoneEntry[];
  pos: Point;
  trail: TrackPoint[];
  reached: ZoneEntry[];
}

interface ZoneInfo {
  name: string;
  points: Point[];
  color: string;
}

const ZONE_PALETTE = [
  "#3b82f6", // blue
  "#10b981", // green
  "#f59e0b", // amber
  "#8b5cf6", // purple
  "#ec4899", // pink
  "#06b6d4", // cyan
];

function formatTime(sec: number): string {
  if (isNaN(sec) || sec < 0) sec = 0;
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  const pad = (n: number) => String(n).padStart(2, "0");
  if (h > 0) {
    return `${pad(h)}:${pad(m)}:${pad(s)}`;
  }
  return `${pad(m)}:${pad(s)}`;
}

const VEHICLE_LABELS = ["car", "truck", "motorcycle"];

/** Alert red, plate purple, vehicle yellow, anything else cyan. */
function trackColor(ev: ForensicEvent, label: string): string {
  if (ev.severity === "alert") return "#ef4444";
  if (ev.plates && ev.plates.length > 0) return "#a855f7";
  return VEHICLE_LABELS.includes(label) ? "#eab308" : "#06b6d4";
}

function trackEmoji(ev: ForensicEvent, label: string): string {
  if (VEHICLE_LABELS.includes(label)) return "🚗";
  return ev.severity === "alert" ? "⚠️" : "👤";
}

/**
 * CameraEvidenceOverlay renders configured Frigate camera zones, the real observed path of each
 * tracked object up to the current playback instant, and a stable event HUD chip. It never draws a
 * trajectory that Frigate did not report.
 */
function CameraEvidenceOverlay({
  cameraId,
  currentTime,
  clipStartUnix,
  cameraEvents,
  manifestZones,
  isPublic,
  layers,
}: {
  cameraId: string;
  currentTime: number;
  clipStartUnix: number;
  cameraEvents: ForensicEvent[];
  manifestZones?: Record<string, any>;
  isPublic?: boolean;
  layers: {
    zones: boolean;
    touched: boolean;
    tracking: boolean;
    events: boolean;
  };
}) {
  // Query camera config for zones when not public
  const configDoc = useQuery({
    ...cameraFrigateDocQuery(cameraId),
    enabled: !!cameraId && !isPublic && !manifestZones?.[cameraId],
  });

  // Resolved zones for this camera — only real configured zones, never fake dummy zones
  const zones: ZoneInfo[] = useMemo(() => {
    const rawZones =
      manifestZones?.[cameraId] ||
      (configDoc.data?.config?.zones as Record<string, any> | undefined);

    if (rawZones && typeof rawZones === "object") {
      const list: ZoneInfo[] = [];
      let idx = 0;
      for (const [name, val] of Object.entries(rawZones)) {
        const coords = (val as any)?.coordinates;
        const pts = parseCoordinates(coords);
        if (pts.length >= 3) {
          list.push({
            name,
            points: pts,
            color: ZONE_PALETTE[idx % ZONE_PALETTE.length] ?? "#3b82f6",
          });
          idx++;
        }
      }
      if (list.length > 0) return list;
    }

    return [];
  }, [manifestZones, cameraId, configDoc.data]);

  // Current active instant in epoch seconds
  const currentInstantUnix = clipStartUnix + currentTime;

  // Active events: any event in a minimum 8s display window around current instant
  const activeEvents = useMemo(() => {
    return cameraEvents.filter((ev) => {
      const st = new Date(ev.start_time).getTime() / 1000;
      const rawEt = ev.end_time ? new Date(ev.end_time).getTime() / 1000 : st + 6;
      const displayDuration = Math.max(8, rawEt - st);
      const displayEnd = st + displayDuration;
      return currentInstantUnix >= st - 0.2 && currentInstantUnix <= displayEnd + 0.5;
    });
  }, [cameraEvents, currentInstantUnix]);

  // Real Frigate tracks of this camera's events and the zones each one entered. Nothing is
  // synthesized: events without tracks draw no trajectory.
  const tracked = useMemo(() => {
    const out: { key: string; ev: ForensicEvent; track: ObjectTrack; entries: ZoneEntry[] }[] = [];
    for (const ev of cameraEvents) {
      for (const track of ev.tracks ?? []) {
        if (track.path.length === 0) continue;
        out.push({ key: `${ev.id}:${track.object_id}`, ev, track, entries: zoneEntries(track, zones) });
      }
    }
    return out;
  }, [cameraEvents, zones]);

  // Tracks whose observed time span contains the current instant, with their state at it.
  const visible = useMemo(() => {
    const out: VisibleTrack[] = [];
    for (const item of tracked) {
      const w = trackWindow(item.track);
      const pos = positionAt(item.track, currentInstantUnix);
      if (!w || !pos) continue;
      out.push({
        ...item,
        pos,
        trail: trailUntil(item.track, currentInstantUnix),
        reached: item.entries.filter((e) => e.t <= currentInstantUnix),
      });
    }
    return out;
  }, [tracked, currentInstantUnix]);

  // Zones currently containing a visible object
  const touchedZoneNames = useMemo(() => {
    const s = new Set<string>();
    for (const v of visible) {
      for (const z of zones) if (z.points.length >= 3 && pointInPolygon(v.pos, z.points)) s.add(z.name);
    }
    return s;
  }, [visible, zones]);

  const hasSvgContent = (layers.zones && zones.length > 0) || (layers.tracking && visible.length > 0);

  return (
    <>
      {/* SVG Layer: real camera zones and the real observed Frigate object tracks */}
      {hasSvgContent && (
        <svg
          viewBox="0 0 1000 1000"
          preserveAspectRatio="none"
          className="absolute inset-0 w-full h-full pointer-events-none z-10 select-none"
        >
          <defs>
            <filter id={`shadow-${cameraId}`} x="-20%" y="-20%" width="140%" height="140%">
              <feDropShadow dx="0" dy="3" stdDeviation="4" floodColor="#000000" floodOpacity="0.85" />
            </filter>
          </defs>

          {/* 1. Real Configured Camera Zones with High-Contrast Legible Pills */}
          {layers.zones &&
            zones.map((zone) => {
              const isTouched = layers.touched && touchedZoneNames.has(zone.name);
              const pointsStr = zone.points
                .map((p) => `${Math.round(p.x * 1000)},${Math.round(p.y * 1000)}`)
                .join(" ");

              const rawCx = zone.points.reduce((a, b) => a + b.x, 0) / zone.points.length;
              const rawCy = zone.points.reduce((a, b) => a + b.y, 0) / zone.points.length;
              const cx = Math.max(140, Math.min(860, Math.round(rawCx * 1000)));
              const cy = Math.max(60, Math.min(940, Math.round(rawCy * 1000)));

              const zoneTitle = isTouched ? `🎯 ${zone.name.toUpperCase()} (ACTIVA)` : zone.name.toUpperCase();
              const pillW = Math.max(160, zoneTitle.length * 16 + 60);
              const pillH = 46;

              return (
                <g key={zone.name} className="transition-all duration-200">
                  <polygon
                    points={pointsStr}
                    fill={isTouched ? "rgba(239, 68, 68, 0.08)" : "none"}
                    stroke={isTouched ? "#ef4444" : zone.color}
                    strokeWidth={isTouched ? "5" : "3"}
                    strokeDasharray={isTouched ? undefined : "12 6"}
                  />

                  {/* High-Contrast Crisp Zone Label Pill (Readable at any scale) */}
                  <g transform={`translate(${cx}, ${cy})`}>
                    <rect
                      x={-pillW / 2}
                      y={-pillH / 2}
                      width={pillW}
                      height={pillH}
                      rx="12"
                      fill={isTouched ? "#dc2626" : "rgba(15, 23, 42, 0.94)"}
                      stroke={isTouched ? "#ffffff" : zone.color}
                      strokeWidth="3.5"
                      filter={`url(#shadow-${cameraId})`}
                    />
                    <circle
                      cx={-pillW / 2 + 20}
                      cy="0"
                      r="8"
                      fill={isTouched ? "#ffffff" : zone.color}
                      stroke="rgba(0,0,0,0.6)"
                      strokeWidth="2"
                    />
                    <text
                      x="10"
                      y="8"
                      textAnchor="middle"
                      fill="#ffffff"
                      fontSize="26"
                      fontWeight="800"
                      letterSpacing="1"
                      className="font-sans select-none"
                    >
                      {zoneTitle}
                    </text>
                  </g>
                </g>
              );
            })}

          {/* 2. Frigate-style object tracking: real observed path, current position, zone entries */}
          {layers.tracking &&
            visible.map(({ key, ev, track, pos, trail, reached }) => {
              const color = trackColor(ev, track.label);
              const toPt = (p: { x: number; y: number }) => `${Math.round(p.x * 1000)},${Math.round(p.y * 1000)}`;
              const start = track.path[0]!;
              const labelText = ev.plates && ev.plates.length > 0 ? `Patente: ${ev.plates[0]}` : labelName(track.label) || "Objeto";
              const tagW = Math.max(160, labelText.length * 16 + 50);
              const cx = Math.round(pos.x * 1000);
              const cy = Math.round(pos.y * 1000);
              const tagX = Math.max(15, Math.min(985 - tagW, cx - tagW / 2));
              const tagY = Math.max(48, cy - 70);

              return (
                <g key={key}>
                  {/* Full observed path, faint */}
                  <polyline
                    points={track.path.map(toPt).join(" ")}
                    fill="none"
                    stroke={color}
                    strokeWidth="2"
                    strokeLinejoin="round"
                    opacity="0.3"
                  />
                  {track.path.map((p, i) => (
                    <circle key={i} cx={Math.round(p.x * 1000)} cy={Math.round(p.y * 1000)} r="3" fill={color} opacity="0.35" />
                  ))}

                  {/* Traversed part, solid */}
                  {trail.length > 1 && (
                    <polyline
                      points={[...trail, pos].map(toPt).join(" ")}
                      fill="none"
                      stroke={color}
                      strokeWidth="4"
                      strokeLinejoin="round"
                      strokeLinecap="round"
                      opacity="0.9"
                    />
                  )}
                  {trail.map((p, i) => (
                    <circle
                      key={i}
                      cx={Math.round(p.x * 1000)}
                      cy={Math.round(p.y * 1000)}
                      r="6"
                      fill={color}
                      stroke="#ffffff"
                      strokeWidth="1.5"
                    />
                  ))}

                  {/* Start marker */}
                  <circle
                    cx={Math.round(start.x * 1000)}
                    cy={Math.round(start.y * 1000)}
                    r="12"
                    fill="#ffffff"
                    stroke={color}
                    strokeWidth="4"
                    filter={`url(#shadow-${cameraId})`}
                  />

                  {/* Zone entries already reached */}
                  {reached.map((e) => {
                    const text = `${e.zone.toUpperCase()} · ${fmtTime(new Date(e.t * 1000))}`;
                    const w = text.length * 15 + 40;
                    const ex = Math.round(e.x * 1000);
                    const ey = Math.round(e.y * 1000);
                    const px = Math.max(15, Math.min(985 - w, ex - w / 2));
                    const py = Math.min(940, ey + 22);
                    return (
                      <g key={`${e.zone}-${e.t}`}>
                        <circle cx={ex} cy={ey} r="11" fill="#ffffff" stroke="#dc2626" strokeWidth="5" filter={`url(#shadow-${cameraId})`} />
                        <g transform={`translate(${px}, ${py})`}>
                          <rect width={w} height="36" rx="8" fill="rgba(15, 23, 42, 0.95)" stroke="#dc2626" strokeWidth="2.5" filter={`url(#shadow-${cameraId})`} />
                          <text x="14" y="25" fill="#ffffff" fontSize="20" fontWeight="bold" className="font-mono select-none">
                            {text}
                          </text>
                        </g>
                      </g>
                    );
                  })}

                  {/* Current position and label */}
                  <circle cx={cx} cy={cy} r="14" fill={color} stroke="#ffffff" strokeWidth="4" filter={`url(#shadow-${cameraId})`} />
                  <g transform={`translate(${tagX}, ${tagY})`}>
                    <rect width={tagW} height="42" rx="9" fill="rgba(15, 23, 42, 0.95)" stroke={color} strokeWidth="2.5" filter={`url(#shadow-${cameraId})`} />
                    <text x="14" y="28" fill="#ffffff" fontSize="22" fontWeight="bold" className="font-mono select-none">
                      {trackEmoji(ev, track.label)} {labelText}
                    </text>
                  </g>
                </g>
              );
            })}
        </svg>
      )}

      {/* Frigate 0.18-style Event Info HUD (Floating chips in top-left corner) */}
      {layers.events && activeEvents.length > 0 && (
        <div className="absolute top-2.5 left-2.5 z-20 pointer-events-none flex flex-col gap-1.5 max-w-[60%] select-none">
          {activeEvents.map((ev) => {
            const isAlert = ev.severity === "alert";
            const isCar = ev.labels.some((l) => l === "car" || l === "truck" || l === "motorcycle");
            const isPerson = ev.labels.some((l) => l === "person");

            return (
              <div
                key={ev.id}
                className="flex flex-wrap items-center gap-1.5 px-2 py-1 rounded-m3-sm bg-surface-0/75 border border-outline-variant/60 shadow-md text-xs"
              >
                <span
                  className={`px-1.5 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider ${
                    isAlert ? "bg-bad text-white" : "bg-primary text-white"
                  }`}
                >
                  {isAlert ? "Alerta" : "Detección"}
                </span>

                <span className="font-semibold text-on-surface flex items-center gap-1 font-mono">
                  <span>{isCar ? "🚗" : isPerson ? "👤" : "🎯"}</span>
                  <span>{ev.labels.map(labelName).join(", ") || "Objeto"}</span>
                </span>

                {ev.plates && ev.plates.length > 0 && (
                  <span className="px-1.5 py-0.5 rounded bg-purple-950/80 text-purple-200 border border-purple-500/40 font-mono text-[11px] font-bold">
                    Patente: {ev.plates[0]}
                  </span>
                )}

                {ev.zones && ev.zones.length > 0 && (
                  <span className="px-1.5 py-0.5 rounded bg-surface-2 text-on-surface-variant border border-outline-variant/40 text-[11px] flex items-center gap-1">
                    <span>🎯</span>
                    <span>{ev.zones.join(", ")}</span>
                  </span>
                )}

                {visible
                  .filter((v) => v.ev.id === ev.id)
                  .flatMap((v) => v.reached)
                  .sort((a, b) => a.t - b.t)
                  // Busy scenes keep one long event open: show only the latest entries so the
                  // chip never grows over the video.
                  .slice(-3)
                  .map((e) => (
                    <span
                      key={`${e.zone}-${e.t}`}
                      className="px-1.5 py-0.5 rounded bg-surface-2 text-on-surface-variant border border-outline-variant/40 text-[11px]"
                    >
                      Entró a {e.zone} {fmtTime(new Date(e.t * 1000))}
                    </span>
                  ))}
              </div>
            );
          })}
        </div>
      )}
    </>
  );
}

export function EvidencePlayerModal({
  job,
  onClose,
  isPublic = false,
  shareToken,
  password,
}: EvidencePlayerModalProps) {
  // Expected clip duration from timestamps
  const expectedDuration = Math.max(
    1,
    (new Date(job.end_time).getTime() - new Date(job.start_time).getTime()) / 1000
  );

  const clipStartUnix = new Date(job.start_time).getTime() / 1000;

  // Player state: AUTOPLAY starts immediately
  const [isPlaying, setIsPlaying] = useState(true);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(expectedDuration);
  const [playbackRate, setPlaybackRate] = useState(1);
  const [isMuted, setIsMuted] = useState(true);
  const [showManifest, setShowManifest] = useState(false);
  const [copiedHash, setCopiedHash] = useState<string | null>(null);
  const [maximizedCameraId, setMaximizedCameraId] = useState<string | null>(null);

  // Layers visibility state (Frigate 0.18 style)
  const [layers, setLayers] = useState({
    zones: true,
    touched: true,
    tracking: true,
    events: true,
  });
  const [layersOpen, setLayersOpen] = useState(false);

  const videoRefs = useRef<Map<string, HTMLVideoElement>>(new Map());
  const isSeekingRef = useRef(false);
  const masterKey = job.items?.[0]?.id || "";

  const items = job.items || [];
  const cameraIds = useMemo(() => items.map((it) => it.camera_id).filter(Boolean), [items]);
  const itemIds = useMemo(() => items.map((it) => it.id), [items]);

  // Synchronize follower playback with the master video element
  const getVideo = useCallback((id: string) => videoRefs.current.get(id), []);
  useSyncedPlayback(masterKey, itemIds, getVideo);

  // Query events for all cameras in the export job
  const allEventsQuery = useQuery({
    queryKey: ["export-all-events", job.id, cameraIds],
    queryFn: async (): Promise<ForensicEvent[]> => {
      if (cameraIds.length === 0) return [];
      const res = await api.GET("/api/v1/events", {
        params: {
          query: {
            camera_id: cameraIds,
            from: job.start_time,
            to: job.end_time,
            overlap: true,
            limit: 300,
          },
        },
      });
      const data = unwrap(res);
      return data.items as ForensicEvent[];
    },
    enabled:
      !!job.id &&
      !isPublic &&
      cameraIds.length > 0 &&
      (!job.manifest?.events || job.manifest.events.length === 0),
  });

  const allEvents: ForensicEvent[] = useMemo(() => {
    if (job.manifest?.events && Array.isArray(job.manifest.events) && job.manifest.events.length > 0) {
      return job.manifest.events;
    }
    return allEventsQuery.data || [];
  }, [job.manifest, allEventsQuery.data]);

  // Video source resolver
  const getVideoSrc = (item: EvidencePlayerCameraItem): string => {
    if (item.video_url) {
      return item.video_url;
    }
    if (isPublic && shareToken) {
      const pwParam = password ? `?password=${encodeURIComponent(password)}` : "";
      return `/media/v1/public/shares/${shareToken}/items/${item.id}/video${pwParam}`;
    }
    return `/media/v1/export-jobs/${job.id}/items/${item.id}/video`;
  };

  const getDownloadHref = (): string => {
    if (job.download_url) {
      return job.download_url;
    }
    if (isPublic && shareToken) {
      const pwParam = password ? `?password=${encodeURIComponent(password)}` : "";
      return `/media/v1/public/shares/${shareToken}/download${pwParam}`;
    }
    return `/media/v1/export-jobs/${job.id}/download`;
  };

  // Next / Previous Event navigation helpers
  const sortedEventTimes = useMemo(() => {
    return allEvents
      .map((e) => new Date(e.start_time).getTime() / 1000 - clipStartUnix)
      .filter((t) => t >= 0 && t <= (duration > 0 ? duration : expectedDuration))
      .sort((a, b) => a - b);
  }, [allEvents, clipStartUnix, duration, expectedDuration]);

  const jumpToNextEvent = () => {
    const next = sortedEventTimes.find((t) => t > currentTime + 1);
    if (next !== undefined) seekTo(next);
  };

  const jumpToPrevEvent = () => {
    const prev = [...sortedEventTimes].reverse().find((t) => t < currentTime - 1);
    if (prev !== undefined) seekTo(prev);
    else seekTo(0);
  };

  // Autoplay on mount: trigger play immediately
  useEffect(() => {
    setIsPlaying(true);
    const timer = setTimeout(() => {
      videoRefs.current.forEach((v) => {
        v.play().catch(() => {});
      });
    }, 100);
    return () => clearTimeout(timer);
  }, []);

  // Real-time playback position ticker (10 Hz): strictly driven by the master video element
  useEffect(() => {
    if (!isPlaying || !masterKey) return;

    const interval = setInterval(() => {
      if (isSeekingRef.current) return;

      const mv = videoRefs.current.get(masterKey);
      if (mv && !mv.paused && !mv.seeking) {
        setCurrentTime(mv.currentTime);
        if (mv.duration && isFinite(mv.duration) && mv.duration > 0) {
          setDuration((prev) => Math.max(prev, mv.duration));
        }
      }
    }, 100);

    return () => clearInterval(interval);
  }, [isPlaying, masterKey]);

  // Sync state strictly from master video HTML events
  const onTimeUpdate = (itemId: string, e: React.SyntheticEvent<HTMLVideoElement>) => {
    if (itemId !== masterKey || isSeekingRef.current) return;
    const target = e.currentTarget;
    setCurrentTime(target.currentTime);
    if (target.duration && isFinite(target.duration) && target.duration > 0) {
      setDuration((prev) => Math.max(prev, target.duration));
    }
  };

  const onLoadedMetadata = (itemId: string, e: React.SyntheticEvent<HTMLVideoElement>) => {
    if (itemId !== masterKey) return;
    const target = e.currentTarget;
    if (target.duration && isFinite(target.duration) && target.duration > 0) {
      setDuration((prev) => Math.max(prev, target.duration));
    }
  };

  const togglePlay = useCallback(() => {
    const willPlay = !isPlaying;
    videoRefs.current.forEach((v) => {
      if (willPlay) {
        v.play().catch(() => {});
      } else {
        v.pause();
      }
    });
    setIsPlaying(willPlay);
  }, [isPlaying]);

  const seekTo = (sec: number) => {
    const maxDur = duration > 0 ? duration : expectedDuration;
    const clamped = Math.max(0, Math.min(sec, maxDur));
    setCurrentTime(clamped);
    videoRefs.current.forEach((v) => {
      try {
        v.currentTime = clamped;
      } catch {}
    });
  };

  const changeSpeed = (rate: number) => {
    setPlaybackRate(rate);
    videoRefs.current.forEach((v) => {
      v.playbackRate = rate;
    });
  };

  const toggleMute = () => {
    const willMute = !isMuted;
    setIsMuted(willMute);
    videoRefs.current.forEach((v) => {
      v.muted = willMute;
    });
  };

  const stepTime = (delta: number) => {
    seekTo(currentTime + delta);
  };

  const copyToClipboard = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopiedHash(id);
    setTimeout(() => setCopiedHash(null), 2000);
  };

  // Keyboard controls: Space to play/pause, Left/Right for -5s/+5s
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) {
        return;
      }
      if (e.code === "Space") {
        e.preventDefault();
        togglePlay();
      } else if (e.code === "ArrowLeft") {
        e.preventDefault();
        stepTime(-5);
      } else if (e.code === "ArrowRight") {
        e.preventDefault();
        stepTime(5);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [togglePlay, currentTime, duration]);

  const gridColsClass =
    items.length <= 1
      ? "grid-cols-1"
      : items.length === 2
        ? "grid-cols-1 md:grid-cols-2"
        : "grid-cols-1 md:grid-cols-2";

  // Playhead percentage for needle cursor
  const playheadPct = Math.min(
    100,
    Math.max(0, (currentTime / (duration > 0 ? duration : expectedDuration)) * 100)
  );

  return (
    <Modal
      title={job.name}
      onClose={onClose}
      className="max-w-6xl max-h-[96vh] bg-surface-0 border border-outline-variant/60 p-5 flex flex-col gap-4"
    >
      {/* Forensic Header Info & Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-3 p-3 bg-surface-1 rounded-m3-md border border-outline-variant/50">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-semibold bg-ok-container text-on-ok-container">
              <Icon icon={ShieldCheck} size="xs" />
              <span>Evidencia Forense Verificada</span>
            </span>
            {job.protected ? (
              <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-primary/10 text-primary border border-primary/20">
                <Icon icon={ShieldCheck} size="xs" />
                <span>Protegida (Permanente)</span>
              </span>
            ) : (
              <span
                className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-warn-container text-on-warn-container border border-warn/30"
                title="Se eliminará automáticamente a los 30 días a menos que se proteja"
              >
                <Icon icon={Clock} size="xs" />
                <span>Auto-borrado en 30 días</span>
              </span>
            )}
            <span className="text-xs text-muted font-mono">
              {items.length} {items.length === 1 ? "cámara" : "cámaras"} • {fmtBytes(job.total_bytes)}
            </span>
          </div>
          <span className="text-xs text-on-surface-variant font-mono">
            Rango: {fmtDateTime(job.start_time)} ➔ {fmtDateTime(job.end_time)}
          </span>
        </div>

        <div className="flex items-center gap-2">
          {/* Layers Toggle Button */}
          <div className="relative">
            <Button
              variant={layersOpen ? "filled" : "outlined"}
              size="sm"
              onClick={() => setLayersOpen(!layersOpen)}
              title="Capas forenses (Zonas y eventos estilo Frigate)"
            >
              <Icon icon={Layers} size="xs" />
              <span>Capas</span>
            </Button>

            {layersOpen && (
              <div className="absolute right-0 top-10 z-50 w-56 p-3 bg-surface-2 rounded-m3-md border border-outline-variant shadow-lg flex flex-col gap-2.5 text-xs">
                <span className="font-semibold text-on-surface border-b border-outline-variant/40 pb-1">
                  Capas estilo Frigate
                </span>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={layers.zones}
                    onChange={(e) => setLayers((l) => ({ ...l, zones: e.target.checked }))}
                    className="accent-primary rounded"
                  />
                  <span>Zonas configuradas</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={layers.touched}
                    onChange={(e) => setLayers((l) => ({ ...l, touched: e.target.checked }))}
                    className="accent-primary rounded"
                  />
                  <span>Resaltar zonas activas</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={layers.tracking}
                    onChange={(e) => setLayers((l) => ({ ...l, tracking: e.target.checked }))}
                    className="accent-primary rounded"
                  />
                  <span>Seguimiento de objetos (Frigate)</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={layers.events}
                    onChange={(e) => setLayers((l) => ({ ...l, events: e.target.checked }))}
                    className="accent-primary rounded"
                  />
                  <span>Ficha de eventos (HUD)</span>
                </label>
              </div>
            )}
          </div>

          {job.manifest && (
            <Button
              variant="outlined"
              size="sm"
              onClick={() => setShowManifest(!showManifest)}
              title="Inspeccionar metadata y firmas SHA-256 del paquete"
            >
              <Icon icon={FileCode2} size="xs" />
              <span>{showManifest ? "Ocultar Manifest" : "Ver Manifest"}</span>
            </Button>
          )}

          <LinkButton
            variant="filled"
            size="sm"
            href={getDownloadHref()}
            title={items.length > 1 ? "Descargar paquete ZIP con manifest" : "Descargar video MP4"}
          >
            <Icon icon={Download} size="xs" />
            <span>{items.length > 1 ? "Descargar ZIP" : "Descargar"}</span>
          </LinkButton>
        </div>
      </div>

      {/* Manifest JSON Drawer */}
      {showManifest && job.manifest && (
        <div className="p-3 bg-surface-2 rounded-m3-md border border-outline-variant/60 font-mono text-xs max-h-48 overflow-auto">
          <div className="flex items-center justify-between pb-2 mb-2 border-b border-outline-variant/40">
            <span className="font-semibold text-primary">manifest.json (Metadata Forense)</span>
            <button
              type="button"
              onClick={() => copyToClipboard(JSON.stringify(job.manifest, null, 2), "manifest")}
              className="inline-flex items-center gap-1 text-[11px] text-muted hover:text-primary transition-colors"
            >
              <Icon icon={copiedHash === "manifest" ? Check : Copy} size="xs" />
              <span>{copiedHash === "manifest" ? "Copiado" : "Copiar JSON"}</span>
            </button>
          </div>
          <pre className="text-[11px] text-on-surface leading-relaxed whitespace-pre-wrap">
            {JSON.stringify(job.manifest, null, 2)}
          </pre>
        </div>
      )}

      {/* Maximized Camera Banner */}
      {maximizedCameraId && (
        <div className="flex items-center justify-between px-3.5 py-2 bg-primary/10 rounded-m3-md border border-primary/20 text-xs">
          <span className="font-semibold text-primary flex items-center gap-1.5">
            <Icon icon={Maximize2} size="xs" />
            <span>
              Cámara maximizada:{" "}
              {items.find((it) => it.id === maximizedCameraId)?.camera_name}
            </span>
          </span>
          <button
            type="button"
            onClick={() => setMaximizedCameraId(null)}
            className="text-xs font-semibold text-primary hover:underline flex items-center gap-1 cursor-pointer"
          >
            <Icon icon={Minimize2} size="xs" />
            <span>Volver a cuadrícula ({items.length} cámaras)</span>
          </button>
        </div>
      )}

      {/* Multi-Camera Video Grid with Forensic Overlays */}
      <div
        className={`grid ${
          maximizedCameraId ? "grid-cols-1" : gridColsClass
        } gap-3 overflow-y-auto ${
          maximizedCameraId ? "max-h-[65vh]" : "max-h-[50vh]"
        } p-1`}
      >
        {(maximizedCameraId ? items.filter((it) => it.id === maximizedCameraId) : items).map((it) => {
          const isCopied = copiedHash === it.id;
          const camEvents = allEvents.filter((e) => e.camera_id === it.camera_id);

          return (
            <div
              key={it.id}
              className="relative flex flex-col bg-surface-1 rounded-m3-lg border border-outline-variant/60 overflow-hidden shadow-sm"
            >
              {/* Camera Header Badge */}
              <div className="flex items-center justify-between px-3 py-1.5 bg-surface-2/80 border-b border-outline-variant/40 text-xs">
                <span className="font-semibold text-on-surface">{it.camera_name}</span>
                <div className="flex items-center gap-2">
                  {it.server_name && (
                    <span className="text-[11px] text-muted font-mono">{it.server_name}</span>
                  )}
                  <button
                    type="button"
                    onClick={() =>
                      setMaximizedCameraId(maximizedCameraId === it.id ? null : it.id)
                    }
                    className="p-1 rounded hover:bg-surface-3 text-muted hover:text-on-surface transition-colors cursor-pointer"
                    title={
                      maximizedCameraId === it.id ? "Volver a cuadrícula" : "Maximizar esta cámara"
                    }
                  >
                    <Icon icon={maximizedCameraId === it.id ? Minimize2 : Maximize2} size="xs" />
                  </button>
                </div>
              </div>

              {/* HTML5 Video Element & Overlay Canvas */}
              <div className="relative aspect-video bg-black flex items-center justify-center overflow-hidden">
                <video
                  ref={(el) => {
                    if (el) {
                      videoRefs.current.set(it.id, el);
                    } else {
                      videoRefs.current.delete(it.id);
                    }
                  }}
                  src={getVideoSrc(it)}
                  muted={isMuted}
                  playsInline
                  autoPlay
                  preload="auto"
                  onTimeUpdate={(e) => onTimeUpdate(it.id, e)}
                  onLoadedMetadata={(e) => onLoadedMetadata(it.id, e)}
                  onCanPlay={(e) => {
                    if (isPlaying) {
                      e.currentTarget.play().catch(() => {});
                    }
                  }}
                  onEnded={() => {
                    if (it.id === masterKey) {
                      setIsPlaying(false);
                    }
                  }}
                  className="w-full h-full object-cover"
                />

                {/* SVG Forensic Layer: Zones, Touched Zones, Tracking Lines */}
                <CameraEvidenceOverlay
                  cameraId={it.camera_id}
                  currentTime={currentTime}
                  clipStartUnix={clipStartUnix}
                  cameraEvents={camEvents}
                  manifestZones={job.manifest?.zones}
                  isPublic={isPublic}
                  layers={layers}
                />
              </div>

              {/* Camera Forensic Integrity Footer */}
              <div className="flex items-center justify-between px-3 py-1.5 bg-surface-1 border-t border-outline-variant/40 text-[11px] font-mono">
                {it.sha256_hash ? (
                  <button
                    type="button"
                    onClick={() => copyToClipboard(it.sha256_hash!, it.id)}
                    className="inline-flex items-center gap-1.5 text-ok hover:text-ok/80 transition-colors"
                    title={`Hash SHA-256 completo: ${it.sha256_hash}\nHaga clic para copiar`}
                  >
                    <Icon icon={isCopied ? Check : CheckCircle2} size="xs" />
                    <span>SHA-256: {it.sha256_hash.slice(0, 16)}...</span>
                  </button>
                ) : (
                  <span className="text-muted">Procesando firma...</span>
                )}
                <span className="text-muted">{fmtBytes(it.total_bytes)}</span>
              </div>
            </div>
          );
        })}
      </div>

      {/* Compact Multi-Camera Dock Timeline (Live Recorded Pattern) */}
      <div className="flex flex-col gap-2 p-3 bg-surface-1 rounded-m3-lg border border-outline-variant/60 shadow-xs">
        {/* Top Header: Clock, Active Counts & Detections Legend */}
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs pb-1 border-b border-outline-variant/30">
          <div className="flex items-center gap-2 font-mono">
            <span className="font-bold text-primary text-sm">{formatTime(currentTime)}</span>
            <span className="text-muted text-xs">/</span>
            <span className="text-muted text-xs">
              {formatTime(duration > 0 ? duration : expectedDuration)}
            </span>
            <span className="text-[11px] text-muted ml-2">
              ({items.length} {items.length === 1 ? "cámara" : "cámaras"} · {allEvents.length}{" "}
              {allEvents.length === 1 ? "evento" : "eventos"})
            </span>
          </div>

          {/* Detections Legend */}
          <div className="flex flex-wrap items-center gap-2.5 text-[10px] text-muted">
            <span className="flex items-center gap-1">
              <span className="size-2 rounded-full bg-bad" />
              <span>Alerta</span>
            </span>
            <span className="flex items-center gap-1">
              <span className="size-2 rounded-full bg-[#06b6d4]" />
              <span>Persona</span>
            </span>
            <span className="flex items-center gap-1">
              <span className="size-2 rounded-full bg-[#eab308]" />
              <span>Vehículo</span>
            </span>
            <span className="flex items-center gap-1">
              <span className="size-2 rounded-full bg-[#a855f7]" />
              <span>LPR</span>
            </span>
          </div>
        </div>

        {/* Unified Timeline Block: Ruler + Range Slider + Stacked Compact Rows */}
        <div className="flex flex-col gap-1">
          {/* Time Ruler above tracks */}
          <div className="flex items-center select-none text-[10px] text-muted font-mono">
            <div className="w-20 sm:w-28 shrink-0 text-right pr-2 text-muted/70">Tiempo</div>
            <div className="relative flex-1 h-3.5 flex items-center">
              {[0, 0.25, 0.5, 0.75, 1].map((pct, i) => (
                <span
                  key={i}
                  style={{
                    left: `${pct * 100}%`,
                    transform:
                      i === 0 ? "none" : i === 4 ? "translateX(-100%)" : "translateX(-50%)",
                  }}
                  className="absolute top-0 whitespace-nowrap text-[10px]"
                >
                  {formatTime(pct * (duration > 0 ? duration : expectedDuration))}
                </span>
              ))}
            </div>
          </div>

          {/* Range Slider for scrubbing directly below ruler */}
          <div className="flex items-center">
            <div className="w-20 sm:w-28 shrink-0" />
            <div className="relative flex-1 flex flex-col justify-center">
              <input
                type="range"
                min={0}
                max={duration > 0 ? duration : expectedDuration}
                step={0.1}
                value={currentTime}
                onPointerDown={() => {
                  isSeekingRef.current = true;
                }}
                onPointerUp={(e) => {
                  isSeekingRef.current = false;
                  seekTo(parseFloat(e.currentTarget.value));
                }}
                onChange={(e) => {
                  const val = parseFloat(e.target.value);
                  setCurrentTime(val);
                  if (!isSeekingRef.current) {
                    seekTo(val);
                  }
                }}
                className="w-full h-1.5 bg-surface-3 rounded-lg appearance-none cursor-pointer accent-primary"
                aria-label="Línea de tiempo"
              />
            </div>
          </div>

          {/* Stacked Camera Rows (Compact, max-h bounded with scroll) */}
          <div className="flex flex-col gap-1 max-h-24 sm:max-h-28 overflow-y-auto pr-1">
            {items.map((it) => {
              const camEvents = allEvents.filter((e) => e.camera_id === it.camera_id);
              const maxD = duration > 0 ? duration : expectedDuration;

              return (
                <div key={it.id} className="flex items-center gap-1.5">
                  {/* Camera Name Label */}
                  <div
                    className="w-20 sm:w-28 text-[11px] font-medium text-on-surface truncate shrink-0 flex items-center gap-1 select-none"
                    title={it.camera_name}
                  >
                    <Icon icon={Camera} size="xs" className="text-muted shrink-0 size-3" />
                    <span className="truncate">{it.camera_name}</span>
                  </div>

                  {/* Camera Timeline Track */}
                  <div
                    className="relative flex-1 h-5 bg-surface-3/80 rounded-[3px] overflow-hidden cursor-pointer border border-outline-variant/30 hover:border-primary/50 transition-colors"
                    onClick={(e) => {
                      const rect = e.currentTarget.getBoundingClientRect();
                      const pct = (e.clientX - rect.left) / rect.width;
                      seekTo(pct * maxD);
                    }}
                    title={`Línea de tiempo de ${it.camera_name} — Clic para reproducir`}
                  >
                    {/* Continuous recording coverage */}
                    <div className="absolute inset-0 bg-primary/15" />

                    {/* Detection blocks */}
                    {camEvents.map((ev, evIdx) => {
                      const st =
                        new Date(ev.start_time).getTime() / 1000 - clipStartUnix;
                      const et =
                        (ev.end_time
                          ? new Date(ev.end_time).getTime() / 1000
                          : new Date(ev.start_time).getTime() / 1000 + 4) -
                        clipStartUnix;
                      const leftPct = Math.min(100, Math.max(0, (st / maxD) * 100));
                      const widthPct = Math.min(
                        100 - leftPct,
                        Math.max(1.5, ((et - st) / maxD) * 100)
                      );

                      const isAlert = ev.severity === "alert";
                      const isPerson = ev.labels.some((l) => l === "person");
                      const isCar = ev.labels.some(
                        (l) => l === "car" || l === "truck" || l === "motorcycle"
                      );
                      const hasPlate = ev.plates && ev.plates.length > 0;

                      const bgColor = isAlert
                        ? "bg-bad text-white"
                        : hasPlate
                        ? "bg-[#a855f7] text-white"
                        : isCar
                        ? "bg-[#eab308] text-black"
                        : isPerson
                        ? "bg-[#06b6d4] text-white"
                        : "bg-primary text-white";

                      return (
                        <div
                          key={`${ev.id}-${evIdx}`}
                          style={{ left: `${leftPct}%`, width: `${widthPct}%` }}
                          className={`absolute top-0.5 bottom-0.5 rounded-[2px] ${bgColor} opacity-95 hover:opacity-100 flex items-center justify-center text-[9px] font-mono font-bold overflow-hidden z-10 shadow-xs`}
                          title={`${fmtTime(ev.start_time)} · ${ev.labels
                            .map(labelName)
                            .join(", ")}${
                            hasPlate ? ` · Patente: ${ev.plates?.[0]}` : ""
                          }${ev.zones?.length ? ` · Zonas: ${ev.zones.join(", ")}` : ""}`}
                        >
                          {hasPlate ? ev.plates?.[0] : isCar ? "🚗" : isPerson ? "👤" : ""}
                        </div>
                      );
                    })}

                    {/* Synchronized Vertical Playhead Needle */}
                    <div
                      style={{ left: `${playheadPct}%` }}
                      className="absolute inset-y-0 w-0.5 bg-white shadow-[0_0_4px_rgba(255,255,255,0.9)] z-20 pointer-events-none transition-none"
                    />
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* Compact Transport Controls */}
        <div className="flex flex-wrap items-center justify-between gap-2 pt-1 border-t border-outline-variant/30">
          <div className="flex items-center gap-1.5 flex-wrap">
            <Button
              variant="filled"
              size="sm"
              onClick={togglePlay}
              title={isPlaying ? "Pausar (Espacio)" : "Reproducir (Espacio)"}
            >
              <Icon icon={isPlaying ? Pause : Play} size="xs" />
              <span>{isPlaying ? "Pausar" : "Reproducir"}</span>
            </Button>

            <Button
              variant="outlined"
              size="sm"
              onClick={() => stepTime(-10)}
              title="Retroceder 10 segundos (Flecha izquierda)"
            >
              <Icon icon={Rewind} size="xs" />
              <span>-10s</span>
            </Button>

            <Button
              variant="outlined"
              size="sm"
              onClick={() => stepTime(10)}
              title="Avanzar 10 segundos (Flecha derecha)"
            >
              <Icon icon={FastForward} size="xs" />
              <span>+10s</span>
            </Button>

            <Button
              variant="outlined"
              size="sm"
              onClick={jumpToPrevEvent}
              disabled={sortedEventTimes.length === 0}
              title="Saltar al evento anterior en la línea de tiempo"
            >
              <Icon icon={SkipBack} size="xs" />
              <span className="hidden sm:inline">Prev</span>
            </Button>

            <Button
              variant="outlined"
              size="sm"
              onClick={jumpToNextEvent}
              disabled={sortedEventTimes.length === 0}
              title="Saltar al siguiente evento en la línea de tiempo"
            >
              <span className="hidden sm:inline">Sig</span>
              <Icon icon={SkipForward} size="xs" />
            </Button>

            <Button
              variant="outlined"
              size="sm"
              onClick={() => seekTo(0)}
              title="Reiniciar reproducción desde el inicio"
            >
              <Icon icon={RotateCcw} size="xs" />
            </Button>

            <Button
              variant="outlined"
              size="sm"
              onClick={toggleMute}
              title={isMuted ? "Activar audio" : "Silenciar audio"}
            >
              <Icon icon={isMuted ? VolumeX : Volume2} size="xs" />
            </Button>
          </div>

          {/* Speed selector */}
          <div className="flex items-center gap-1 text-xs">
            <span className="text-muted mr-1 text-[11px]">Velocidad:</span>
            {[0.5, 1, 2, 4].map((rate) => (
              <button
                key={rate}
                type="button"
                onClick={() => changeSpeed(rate)}
                className={`px-1.5 py-0.5 rounded-m3-sm font-mono text-xs font-medium transition-colors cursor-pointer ${
                  playbackRate === rate
                    ? "bg-primary text-on-primary"
                    : "bg-surface-2 text-on-surface hover:bg-surface-3"
                }`}
              >
                {rate}x
              </button>
            ))}
          </div>
        </div>
      </div>
    </Modal>
  );
}
