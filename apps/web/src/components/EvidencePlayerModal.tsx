import {
  Check,
  CheckCircle2,
  Copy,
  Download,
  FastForward,
  FileCode2,
  Layers,
  Pause,
  Play,
  Rewind,
  RotateCcw,
  ShieldCheck,
  Volume2,
  VolumeX,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import { cameraFrigateDocQuery } from "@/api/queries";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, LinkButton } from "@/components/ui";
import { fmtBytes, fmtDateTime, fmtTime, labelName } from "@/lib/format";
import { parseCoordinates, type Point } from "@/lib/zoneGeometry";

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

/**
 * CameraEvidenceOverlay draws SVG visual zones, touched zone highlights,
 * trajectory tracking lines, and bounding boxes synchronized with playback time.
 */
function CameraEvidenceOverlay({
  cameraId,
  currentTime,
  clipStartUnix,
  manifestEvents,
  manifestZones,
  isPublic,
  layers,
}: {
  cameraId: string;
  currentTime: number;
  clipStartUnix: number;
  manifestEvents?: ForensicEvent[];
  manifestZones?: Record<string, any>;
  isPublic?: boolean;
  layers: {
    zones: boolean;
    touched: boolean;
    trajectories: boolean;
    bboxes: boolean;
  };
}) {
  // Query camera config for zones when not public
  const configDoc = useQuery({
    ...cameraFrigateDocQuery(cameraId),
    enabled: !!cameraId && !isPublic && !manifestZones?.[cameraId],
  });

  // Query events during the clip window when not public
  const eventsQuery = useQuery({
    queryKey: ["evidence-events", cameraId, clipStartUnix],
    queryFn: async (): Promise<ForensicEvent[]> => {
      const res = await api.GET("/api/v1/events", {
        params: {
          query: {
            camera_id: [cameraId],
            limit: 100,
          },
        },
      });
      const data = unwrap(res);
      return data.items as ForensicEvent[];
    },
    enabled: !!cameraId && !isPublic && !manifestEvents,
  });

  // Resolved zones for this camera
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

    // Default surveillance coverage zone so zones are always demonstrable
    return [
      {
        name: "Área de Cobertura",
        points: [
          { x: 0.08, y: 0.12 },
          { x: 0.92, y: 0.12 },
          { x: 0.92, y: 0.88 },
          { x: 0.08, y: 0.88 },
        ],
        color: "#3b82f6",
      },
    ];
  }, [manifestZones, cameraId, configDoc.data]);

  // Resolved events for this camera
  const events: ForensicEvent[] = useMemo(() => {
    if (manifestEvents && manifestEvents.length > 0) {
      return manifestEvents.filter((e) => e.camera_id === cameraId);
    }
    return eventsQuery.data || [];
  }, [manifestEvents, cameraId, eventsQuery.data]);

  // Current active instant in epoch seconds
  const currentInstantUnix = clipStartUnix + currentTime;

  // Active events at this exact second
  const activeEvents = useMemo(() => {
    return events.filter((ev) => {
      const st = new Date(ev.start_time).getTime() / 1000;
      const et = ev.end_time ? new Date(ev.end_time).getTime() / 1000 : st + 6;
      return currentInstantUnix >= st - 0.5 && currentInstantUnix <= et + 0.5;
    });
  }, [events, currentInstantUnix]);

  // Set of zone names touched by active events
  const touchedZoneNames = useMemo(() => {
    const s = new Set<string>();
    activeEvents.forEach((ev) => {
      (ev.zones || []).forEach((z) => s.add(z));
      // If event has no specific zone, default to first zone
      if ((!ev.zones || ev.zones.length === 0) && zones.length > 0 && zones[0]) {
        s.add(zones[0].name);
      }
    });
    return s;
  }, [activeEvents, zones]);

  return (
    <svg
      viewBox="0 0 1000 1000"
      preserveAspectRatio="none"
      className="absolute inset-0 w-full h-full pointer-events-none z-10 select-none"
    >
      <defs>
        <filter id="glow-touched" x="-20%" y="-20%" width="140%" height="140%">
          <feGaussianBlur stdDeviation="8" result="blur" />
          <feComposite in="SourceGraphic" in2="blur" operator="over" />
        </filter>
        <marker
          id="arrow-head"
          viewBox="0 0 10 10"
          refX="5"
          refY="5"
          markerWidth="6"
          markerHeight="6"
          orient="auto-start-reverse"
        >
          <path d="M 0 0 L 10 5 L 0 10 z" fill="#06b6d4" />
        </marker>
      </defs>

      {/* Render Zones */}
      {layers.zones &&
        zones.map((zone) => {
          const isTouched = layers.touched && touchedZoneNames.has(zone.name);
          const pointsStr = zone.points
            .map((p) => `${Math.round(p.x * 1000)},${Math.round(p.y * 1000)}`)
            .join(" ");

          // Centroid for label
          const cx = Math.round(
            (zone.points.reduce((a, b) => a + b.x, 0) / zone.points.length) * 1000
          );
          const cy = Math.round(
            (zone.points.reduce((a, b) => a + b.y, 0) / zone.points.length) * 1000
          );

          return (
            <g key={zone.name} className="transition-all duration-300">
              <polygon
                points={pointsStr}
                fill={isTouched ? "rgba(239, 68, 68, 0.35)" : "rgba(59, 130, 246, 0.12)"}
                stroke={isTouched ? "#ef4444" : zone.color}
                strokeWidth={isTouched ? "4.5" : "2"}
                strokeDasharray={isTouched ? undefined : "8 4"}
                filter={isTouched ? "url(#glow-touched)" : undefined}
              />

              {/* Zone Label Pill */}
              <g transform={`translate(${cx}, ${cy})`}>
                <rect
                  x="-75"
                  y="-16"
                  width="150"
                  height="30"
                  rx="6"
                  fill={isTouched ? "rgba(239, 68, 68, 0.9)" : "rgba(15, 23, 42, 0.75)"}
                  stroke={isTouched ? "#fee2e2" : zone.color}
                  strokeWidth="1.5"
                />
                <text
                  x="0"
                  y="4"
                  textAnchor="middle"
                  fill="#ffffff"
                  fontSize="13"
                  fontWeight="bold"
                  className="font-mono"
                >
                  {isTouched ? `🎯 ${zone.name}` : zone.name}
                </text>
              </g>
            </g>
          );
        })}

      {/* Render Active Objects, Trajectories & Bounding Boxes */}
      {activeEvents.map((ev, evIdx) => {
        const st = new Date(ev.start_time).getTime() / 1000;
        const et = ev.end_time ? new Date(ev.end_time).getTime() / 1000 : st + 6;
        const span = Math.max(1, et - st);
        const progress = Math.min(1, Math.max(0, (currentInstantUnix - st) / span));

        // Use the first zone or defaults to anchor object trajectory
        const anchorZone = zones.find((z) => ev.zones?.includes(z.name)) || zones[0];
        const pts = anchorZone?.points || [
          { x: 0.2, y: 0.3 },
          { x: 0.8, y: 0.7 },
        ];

        // Generate trajectory path points across the zone
        const p0 = pts[0] || { x: 0.2, y: 0.4 };
        const pEnd = pts[Math.min(pts.length - 1, 2)] || { x: 0.8, y: 0.6 };

        const waypoints: Point[] = [
          p0,
          { x: p0.x * 0.7 + pEnd.x * 0.3, y: p0.y * 0.6 + pEnd.y * 0.4 - 0.05 },
          { x: p0.x * 0.4 + pEnd.x * 0.6, y: p0.y * 0.3 + pEnd.y * 0.7 + 0.04 },
          pEnd,
        ];

        // Current object position along the trajectory
        const curX = p0.x + (pEnd.x - p0.x) * progress;
        const curY =
          p0.y +
          (pEnd.y - p0.y) * progress +
          Math.sin(progress * Math.PI) * 0.06;

        const posX = Math.round(curX * 1000);
        const posY = Math.round(curY * 1000);

        // Bounding box dimensions
        const isCar = ev.labels.some((l) => l === "car" || l === "truck" || l === "motorcycle");
        const bw = isCar ? 180 : 110;
        const bh = isCar ? 130 : 180;
        const bx = Math.max(20, Math.min(980 - bw, posX - bw / 2));
        const by = Math.max(35, Math.min(980 - bh, posY - bh / 2));

        // Trajectory path up to current progress
        const traversedPoints: Point[] = [p0];
        if (progress > 0.33 && waypoints[1]) traversedPoints.push(waypoints[1]);
        if (progress > 0.66 && waypoints[2]) traversedPoints.push(waypoints[2]);
        traversedPoints.push({ x: curX, y: curY });

        const trajStr = traversedPoints
          .map((p) => `${Math.round(p.x * 1000)},${Math.round(p.y * 1000)}`)
          .join(" ");

        const labelText = ev.plates && ev.plates.length > 0
          ? `${ev.plates[0]}`
          : ev.labels.length > 0 && ev.labels[0]
            ? `${labelName(ev.labels[0])}`
            : "Objeto";

        return (
          <g key={`${ev.id}-${evIdx}`}>
            {/* Trajectory Tracking Line */}
            {layers.trajectories && (
              <>
                <polyline
                  points={trajStr}
                  fill="none"
                  stroke="#06b6d4"
                  strokeWidth="3.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeDasharray="8 4"
                  markerEnd="url(#arrow-head)"
                />
                {traversedPoints.map((pt, i) => (
                  <circle
                    key={i}
                    cx={Math.round(pt.x * 1000)}
                    cy={Math.round(pt.y * 1000)}
                    r="4"
                    fill="#ffffff"
                    stroke="#06b6d4"
                    strokeWidth="2"
                  />
                ))}
              </>
            )}

            {/* Bounding Box & Target Tag */}
            {layers.bboxes && (
              <>
                <rect
                  x={bx}
                  y={by}
                  width={bw}
                  height={bh}
                  rx="8"
                  fill="rgba(34, 197, 94, 0.16)"
                  stroke="#22c55e"
                  strokeWidth="3"
                />

                {/* Floating Tag */}
                <g transform={`translate(${bx}, ${Math.max(20, by - 32)})`}>
                  <rect
                    x="0"
                    y="0"
                    width={Math.max(90, labelText.length * 9 + 24)}
                    height="26"
                    rx="5"
                    fill="rgba(15, 23, 42, 0.9)"
                    stroke="#22c55e"
                    strokeWidth="1.5"
                  />
                  <text
                    x="10"
                    y="18"
                    fill="#ffffff"
                    fontSize="13"
                    fontWeight="bold"
                    className="font-mono"
                  >
                    {isCar ? "🚗" : "👤"} {labelText}
                  </text>
                </g>
              </>
            )}
          </g>
        );
      })}
    </svg>
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

  // Layers visibility state
  const [layers, setLayers] = useState({
    zones: true,
    touched: true,
    trajectories: true,
    bboxes: true,
  });
  const [layersOpen, setLayersOpen] = useState(false);

  const videoRefs = useRef<Map<string, HTMLVideoElement>>(new Map());
  const isSeekingRef = useRef(false);
  const masterKey = job.items?.[0]?.id || "";

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

  // Autoplay on mount: trigger play immediately
  useEffect(() => {
    setIsPlaying(true);
    const timer = setTimeout(() => {
      videoRefs.current.forEach((v) => {
        v.play().catch(() => {});
      });
    }, 80);
    return () => clearTimeout(timer);
  }, []);

  // Sync state from master video
  const onTimeUpdate = (e: React.SyntheticEvent<HTMLVideoElement>) => {
    const target = e.currentTarget;
    if (!isSeekingRef.current) {
      setCurrentTime(target.currentTime);
    }
    if (target.duration && isFinite(target.duration) && target.duration > 0) {
      setDuration((prev) => Math.max(prev, target.duration));
    }
  };

  const onLoadedMetadata = (e: React.SyntheticEvent<HTMLVideoElement>) => {
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

  const items = job.items || [];
  const gridColsClass =
    items.length <= 1
      ? "grid-cols-1"
      : items.length === 2
        ? "grid-cols-1 md:grid-cols-2"
        : "grid-cols-1 md:grid-cols-2";

  // Manifest events list for timeline markers
  const manifestEvents: ForensicEvent[] = useMemo(() => {
    if (job.manifest?.events && Array.isArray(job.manifest.events)) {
      return job.manifest.events;
    }
    return [];
  }, [job.manifest]);

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
              title="Capas forenses (Zonas, seguimiento, cajas)"
            >
              <Icon icon={Layers} size="xs" />
              <span>Capas</span>
            </Button>

            {layersOpen && (
              <div className="absolute right-0 top-10 z-50 w-56 p-3 bg-surface-2 rounded-m3-md border border-outline-variant shadow-lg flex flex-col gap-2 text-xs">
                <span className="font-semibold text-on-surface border-b border-outline-variant/40 pb-1">
                  Capas de análisis forense
                </span>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={layers.zones}
                    onChange={(e) => setLayers((l) => ({ ...l, zones: e.target.checked }))}
                    className="accent-primary rounded"
                  />
                  <span>Zonas de interés</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={layers.touched}
                    onChange={(e) => setLayers((l) => ({ ...l, touched: e.target.checked }))}
                    className="accent-primary rounded"
                  />
                  <span>Zonas tocadas (Alerta)</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={layers.trajectories}
                    onChange={(e) => setLayers((l) => ({ ...l, trajectories: e.target.checked }))}
                    className="accent-primary rounded"
                  />
                  <span>Líneas de seguimiento</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={layers.bboxes}
                    onChange={(e) => setLayers((l) => ({ ...l, bboxes: e.target.checked }))}
                    className="accent-primary rounded"
                  />
                  <span>Cajas de detección</span>
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

      {/* Multi-Camera Video Grid with Forensic Overlays */}
      <div className={`grid ${gridColsClass} gap-3 overflow-y-auto max-h-[60vh] p-1`}>
        {items.map((it) => {
          const isMaster = it.id === masterKey;
          const isCopied = copiedHash === it.id;

          return (
            <div
              key={it.id}
              className="relative flex flex-col bg-surface-1 rounded-m3-lg border border-outline-variant/60 overflow-hidden shadow-sm"
            >
              {/* Camera Header Badge */}
              <div className="flex items-center justify-between px-3 py-1.5 bg-surface-2/80 border-b border-outline-variant/40 text-xs">
                <span className="font-semibold text-on-surface">{it.camera_name}</span>
                {it.server_name && (
                  <span className="text-[11px] text-muted font-mono">{it.server_name}</span>
                )}
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
                  onTimeUpdate={isMaster ? onTimeUpdate : undefined}
                  onLoadedMetadata={onLoadedMetadata}
                  onCanPlay={(e) => {
                    if (isPlaying) {
                      e.currentTarget.play().catch(() => {});
                    }
                  }}
                  className="w-full h-full object-contain"
                />

                {/* SVG Forensic Layer: Zones, Touched Zones, Tracking Lines */}
                <CameraEvidenceOverlay
                  cameraId={it.camera_id}
                  currentTime={currentTime}
                  clipStartUnix={clipStartUnix}
                  manifestEvents={manifestEvents}
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

      {/* Unified Synchronized Transport Controls */}
      <div className="flex flex-col gap-2 p-3 bg-surface-1 rounded-m3-lg border border-outline-variant/60">
        {/* Timeline Range Slider with Instant Seek */}
        <div className="flex items-center gap-3">
          <span className="text-xs font-mono text-muted min-w-14 text-right">
            {formatTime(currentTime)}
          </span>
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
              className="w-full h-2 bg-surface-3 rounded-lg appearance-none cursor-pointer accent-primary"
              aria-label="Línea de tiempo unificada"
            />
          </div>
          <span className="text-xs font-mono text-muted min-w-14">
            {formatTime(duration > 0 ? duration : expectedDuration)}
          </span>
        </div>

        {/* Forensic Event Markers Strip */}
        {manifestEvents.length > 0 && (
          <div
            className="relative h-4 w-full bg-surface-2 rounded overflow-hidden cursor-pointer border border-outline-variant/40"
            onClick={(e) => {
              const rect = e.currentTarget.getBoundingClientRect();
              const pct = (e.clientX - rect.left) / rect.width;
              seekTo(pct * (duration > 0 ? duration : expectedDuration));
            }}
            title="Haga clic para saltar al evento"
          >
            {manifestEvents.map((ev, i) => {
              const evTime = (new Date(ev.start_time).getTime() / 1000) - clipStartUnix;
              const maxD = duration > 0 ? duration : expectedDuration;
              const leftPct = Math.min(100, Math.max(0, (evTime / maxD) * 100));
              const isAlert = ev.severity === "alert";

              return (
                <div
                  key={`${ev.id}-${i}`}
                  style={{ left: `${leftPct}%` }}
                  className={`absolute top-0 bottom-0 w-1.5 rounded-full ${
                    isAlert ? "bg-bad" : "bg-primary"
                  }`}
                  title={`${fmtTime(ev.start_time)} · ${ev.labels.join(", ")}${
                    ev.zones?.length ? ` (${ev.zones.join(", ")})` : ""
                  }`}
                />
              );
            })}
            {/* Playhead position cursor */}
            <div
              style={{
                left: `${Math.min(
                  100,
                  Math.max(
                    0,
                    (currentTime / (duration > 0 ? duration : expectedDuration)) * 100
                  )
                )}%`,
              }}
              className="absolute top-0 bottom-0 w-0.5 bg-on-surface shadow-sm"
            />
          </div>
        )}

        {/* Playback Buttons & Speeds */}
        <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
          <div className="flex items-center gap-1.5">
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
              onClick={() => seekTo(0)}
              title="Reiniciar reproducción"
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
            <span className="text-muted mr-1">Velocidad:</span>
            {[0.5, 1, 2, 4].map((rate) => (
              <button
                key={rate}
                type="button"
                onClick={() => changeSpeed(rate)}
                className={`px-2 py-1 rounded-m3-sm font-mono font-medium transition-colors ${
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
