import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  Eye, Pin, PinOff, Maximize2, Minimize2, History, LayoutGrid, Sparkles,
  CarFront, Bell, Check, X, ShieldAlert, Bug, Layers,
} from "lucide-react";
import { api } from "@/api/client";
import { MsePlayer } from "@/components/MsePlayer";
import { Button, IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";

export type VisionMode = "matrix" | "focus" | "lpr" | "alarm";

interface VisionEventRecord {
  id: string;
  cameraId: string;
  cameraName: string;
  object: string;
  confidence: number;
  zone: string;
  score: number;
  alarm?: string;
  plate?: string;
  vehicleType?: string;
  vehicleColor?: string;
  watchlist?: boolean;
  durationSeconds: number;
  breakdown: Record<string, number>;
}

export function VisionAI() {
  const navigate = useNavigate();

  // Queries
  const camerasQuery = useQuery({
    queryKey: ["cameras"],
    queryFn: async () => {
      const res = await api.GET("/api/v1/cameras");
      return res.data?.items ?? [];
    },
  });

  const cameras = useMemo(() => camerasQuery.data ?? [], [camerasQuery.data]);

  // State
  const [mode, setMode] = useState<VisionMode>("matrix");
  const [layoutCount, setLayoutCount] = useState<number>(4);
  const [fullscreen, setFullscreen] = useState(false);
  const [debugMode, setDebugMode] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [pinnedSlots, setPinnedSlots] = useState<Record<number, boolean>>({});
  const [focusHeroIndex, setFocusHeroIndex] = useState<number>(0);

  // Active object filters
  const [activeObjects, setActiveObjects] = useState<Record<string, boolean>>({
    person: true,
    car: true,
    motorcycle: true,
    truck: false,
    license_plate: true,
    face: false,
  });

  // Presets
  const applyPreset = (preset: string) => {
    switch (preset) {
      case "personas":
        setActiveObjects({ person: true, car: false, motorcycle: false, truck: false, license_plate: false, face: true });
        setMode("matrix");
        break;
      case "vehiculos":
        setActiveObjects({ person: false, car: true, motorcycle: true, truck: true, license_plate: true, face: false });
        setMode("matrix");
        break;
      case "lpr":
        setActiveObjects({ person: false, car: true, motorcycle: false, truck: true, license_plate: true, face: false });
        setMode("lpr");
        break;
      case "alarmas":
        setMode("alarm");
        break;
      case "todo":
      default:
        setActiveObjects({ person: true, car: true, motorcycle: true, truck: true, license_plate: true, face: true });
        setMode("matrix");
        break;
    }
  };

  // Synthetic or Live Dynamic Ranked Events
  const events: VisionEventRecord[] = useMemo(() => {
    if (cameras.length === 0) return [];

    const base: VisionEventRecord[] = [
      {
        id: "ev-1",
        cameraId: cameras[0]?.id ?? "c1",
        cameraName: cameras[0]?.display_name ?? "Acceso Norte",
        object: "person",
        confidence: 0.96,
        zone: "Perímetro Restringido",
        score: 94,
        alarm: "critical",
        durationSeconds: 14,
        breakdown: { object: 40, zone: 30, confidence: 19, alarm: 100 },
      },
      {
        id: "ev-2",
        cameraId: cameras[1]?.id ?? "c2",
        cameraName: cameras[1]?.display_name ?? "Portón Sur",
        object: "car",
        confidence: 0.91,
        zone: "Ingreso Vehicular",
        score: 82,
        plate: "AB123CD",
        vehicleType: "PICKUP",
        vehicleColor: "BLANCO",
        watchlist: true,
        durationSeconds: 26,
        breakdown: { object: 20, confidence: 18, watchlist: 80 },
      },
      {
        id: "ev-3",
        cameraId: cameras[2]?.id ?? "c3",
        cameraName: cameras[2]?.display_name ?? "Perímetro Este",
        object: "motorcycle",
        confidence: 0.88,
        zone: "Perímetro",
        score: 71,
        durationSeconds: 8,
        breakdown: { object: 25, zone: 30, confidence: 17 },
      },
      {
        id: "ev-4",
        cameraId: cameras[3]?.id ?? "c4",
        cameraName: cameras[3]?.display_name ?? "Estacionamiento",
        object: "person",
        confidence: 0.94,
        zone: "Estacionamiento",
        score: 65,
        durationSeconds: 32,
        breakdown: { object: 40, confidence: 19, already_visible_penalty: -15 },
      },
      {
        id: "ev-5",
        cameraId: cameras[4]?.id ?? "c5",
        cameraName: cameras[4]?.display_name ?? "Ruta Acceso",
        object: "truck",
        confidence: 0.89,
        zone: "Exterior",
        score: 55,
        durationSeconds: 4,
        breakdown: { object: 25, confidence: 18 },
      },
    ];

    // Filter by mode and active objects
    return base.filter((ev) => {
      if (mode === "alarm") return Boolean(ev.alarm);
      if (mode === "lpr") return Boolean(ev.plate);
      return activeObjects[ev.object] ?? true;
    });
  }, [cameras, activeObjects, mode]);

  const togglePin = (slotIndex: number) => {
    setPinnedSlots((prev) => ({ ...prev, [slotIndex]: !prev[slotIndex] }));
  };

  const toggleFilter = (obj: string) => {
    setActiveObjects((prev) => ({ ...prev, [obj]: !prev[obj] }));
  };

  return (
    <div className={cn("flex flex-col h-full min-h-0 bg-surface-dim", fullscreen && "fixed inset-0 z-50 p-2 bg-black")}>
      {/* Top Navigation & Controls Bar */}
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-line bg-surface px-4 py-2.5 rounded-m3-lg mb-2 shrink-0">
        <div className="flex flex-wrap items-center gap-3">
          {/* Logo & Status */}
          <div className="flex items-center gap-2">
            <span className="flex size-7 items-center justify-center rounded-m3-sm bg-primary/10 text-primary">
              <Eye className="size-4" />
            </span>
            <div className="flex items-center gap-1.5 font-bold text-sm tracking-tight text-ink">
              <span>VISION AI</span>
              <span className="flex size-2 rounded-full bg-emerald-500 animate-pulse" />
              <span className="text-[10px] font-mono uppercase tracking-wider text-emerald-500 font-semibold">EN VIVO</span>
            </div>
          </div>

          <div className="h-4 w-px bg-line" />

          {/* Quick Presets */}
          <div className="flex items-center gap-1">
            <Button size="sm" variant="tonal" onClick={() => applyPreset("todo")}>Todo</Button>
            <Button size="sm" variant="tonal" onClick={() => applyPreset("personas")}>Personas</Button>
            <Button size="sm" variant="tonal" onClick={() => applyPreset("vehiculos")}>Vehículos</Button>
            <Button size="sm" variant="tonal" onClick={() => applyPreset("lpr")}>LPR</Button>
            <Button size="sm" variant="tonal" onClick={() => applyPreset("alarmas")}>Alarmas</Button>
          </div>

          <div className="h-4 w-px bg-line" />

          {/* Dynamic Object Pills */}
          <div className="flex flex-wrap items-center gap-1">
            {[
              { key: "person", label: "Persona" },
              { key: "car", label: "Auto" },
              { key: "motorcycle", label: "Moto" },
              { key: "truck", label: "Camión" },
              { key: "license_plate", label: "LPR" },
              { key: "face", label: "Rostro" },
            ].map(({ key, label }) => {
              const active = Boolean(activeObjects[key]);
              return (
                <button
                  key={key}
                  type="button"
                  data-active={active ? "true" : "false"}
                  onClick={() => toggleFilter(key)}
                  className={cn(
                    "flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium transition-colors border outline-none",
                    active
                      ? "bg-primary text-on-primary border-primary shadow-xs"
                      : "bg-surface-2 text-on-surface-variant border-line hover:bg-surface-3",
                  )}
                >
                  {label} {active && <Check className="size-3" />}
                </button>
              );
            })}
          </div>
        </div>

        <div className="flex items-center gap-2">
          {/* Mode Selector */}
          <div className="flex items-center rounded-m3-md border border-line bg-surface-2 p-0.5">
            <button
              type="button"
              onClick={() => setMode("matrix")}
              className={cn("flex items-center gap-1 px-2 py-1 rounded-m3-sm text-xs font-medium transition-colors", mode === "matrix" && "bg-surface text-primary shadow-xs")}
            >
              <LayoutGrid className="size-3.5" />
              <span>Matriz</span>
            </button>
            <button
              type="button"
              onClick={() => setMode("focus")}
              className={cn("flex items-center gap-1 px-2 py-1 rounded-m3-sm text-xs font-medium transition-colors", mode === "focus" && "bg-surface text-primary shadow-xs")}
            >
              <Sparkles className="size-3.5" />
              <span>Focus</span>
            </button>
            <button
              type="button"
              onClick={() => setMode("lpr")}
              className={cn("flex items-center gap-1 px-2 py-1 rounded-m3-sm text-xs font-medium transition-colors", mode === "lpr" && "bg-surface text-primary shadow-xs")}
            >
              <CarFront className="size-3.5" />
              <span>Modo LPR</span>
            </button>
            <button
              type="button"
              onClick={() => setMode("alarm")}
              className={cn("flex items-center gap-1 px-2 py-1 rounded-m3-sm text-xs font-medium transition-colors", mode === "alarm" && "bg-surface text-primary shadow-xs")}
            >
              <Bell className="size-3.5" />
              <span>Alarmas</span>
            </button>
          </div>

          {/* Matrix size selectors */}
          {mode === "matrix" && (
            <div className="flex items-center rounded-m3-md border border-line bg-surface-2 p-0.5">
              {[1, 4, 9, 16].map((count) => (
                <button
                  key={count}
                  type="button"
                  onClick={() => setLayoutCount(count)}
                  className={cn("px-2 py-0.5 rounded-m3-sm text-xs font-mono font-medium", layoutCount === count && "bg-surface text-primary shadow-xs")}
                >
                  {count === 1 ? "1" : count === 4 ? "2×2" : count === 9 ? "3×3" : "4×4"}
                </button>
              ))}
            </div>
          )}

          {/* Debug Score toggle */}
          <IconButton
            icon={Bug}
            title={debugMode ? "Desactivar Debug Score" : "Debug Score"}
            aria-label="Debug Score"
            onClick={() => setDebugMode(!debugMode)}
            className={cn("size-8", debugMode && "text-primary bg-primary/10")}
          />

          {/* Active Events Drawer toggle */}
          <Button
            size="sm"
            variant="tonal"
            onClick={() => setSidebarOpen(!sidebarOpen)}
            className="flex items-center gap-1.5"
          >
            <span>Eventos Activos</span>
            <span className="flex items-center justify-center size-4 rounded-full bg-primary text-on-primary text-[10px] font-bold">
              {events.length}
            </span>
          </Button>

          {/* Fullscreen */}
          <IconButton
            icon={fullscreen ? Minimize2 : Maximize2}
            title={fullscreen ? "Salir de pantalla completa" : "Pantalla completa"}
            aria-label="Pantalla completa"
            onClick={() => setFullscreen(!fullscreen)}
            className="size-8"
          />
        </div>
      </header>

      {/* Main Workspace Body */}
      <div className="flex flex-1 min-h-0 gap-2 overflow-hidden">
        {/* Visual Slots View */}
        <div className="flex flex-col flex-1 min-h-0 min-w-0 bg-video rounded-m3-lg overflow-hidden border border-line relative">
          {mode === "focus" ? (
            /* Focus Mode: 1 Hero Slot + Strip of Thumbnails */
            <div className="flex flex-col h-full min-h-0 gap-1.5 p-1.5">
              <div data-testid="vision-hero-slot" className="flex-1 min-h-0 relative rounded-m3-md overflow-hidden bg-black/60 border border-line/60">
                {events[focusHeroIndex] ? (
                  <SlotRenderer
                    event={events[focusHeroIndex]!}
                    slotIndex={0}
                    pinned={Boolean(pinnedSlots[0])}
                    onTogglePin={() => togglePin(0)}
                    debug={debugMode}
                    quality="main"
                    onPlayback={(id) => void navigate({ to: "/playback", search: { camera: id } })}
                  />
                ) : (
                  <div className="flex h-full items-center justify-center text-muted text-sm">Esperando evento prioritario...</div>
                )}
              </div>

              {/* Sub-strip of 4 events */}
              <div data-testid="vision-sub-strip" className="h-32 shrink-0 grid grid-cols-4 gap-1.5">
                {events.slice(0, 4).map((ev, i) => (
                  <div
                    key={ev.id}
                    onClick={() => setFocusHeroIndex(i)}
                    className={cn(
                      "relative rounded-m3-md overflow-hidden bg-black/40 border border-line cursor-pointer hover:border-primary transition-colors",
                      focusHeroIndex === i && "ring-2 ring-primary",
                    )}
                  >
                    <SlotRenderer
                      event={ev}
                      slotIndex={i + 1}
                      pinned={Boolean(pinnedSlots[i + 1])}
                      onTogglePin={() => togglePin(i + 1)}
                      debug={debugMode}
                      quality="sub"
                      compact
                      onPlayback={(id) => void navigate({ to: "/playback", search: { camera: id } })}
                    />
                  </div>
                ))}
              </div>
            </div>
          ) : mode === "lpr" ? (
            /* Specialized LPR Mode */
            <div className="grid grid-cols-1 md:grid-cols-2 gap-2 p-2 h-full overflow-y-auto">
              {events.filter((e) => e.plate).map((ev, i) => (
                <div key={ev.id} className="flex flex-col rounded-m3-lg bg-surface border border-line overflow-hidden shadow-xs">
                  <div className="relative aspect-video bg-black/80">
                    <SlotRenderer
                      event={ev}
                      slotIndex={i}
                      pinned={Boolean(pinnedSlots[i])}
                      onTogglePin={() => togglePin(i)}
                      debug={debugMode}
                      quality="main"
                      onPlayback={(id) => void navigate({ to: "/playback", search: { camera: id } })}
                    />
                  </div>
                  <div data-testid="vision-lpr-hud" className="p-3 bg-surface-2 flex items-center justify-between gap-3 border-t border-line">
                    <div>
                      <div className="text-[10px] uppercase font-mono tracking-wider text-muted">Patente Detectada</div>
                      <div className="text-xl font-bold font-mono text-ink tracking-widest">{ev.plate}</div>
                    </div>
                    <div className="text-right">
                      <div className="text-xs font-semibold text-primary">{ev.vehicleType} · {ev.vehicleColor}</div>
                      <div className="text-[10px] text-muted">Confianza: {Math.round(ev.confidence * 100)}%</div>
                    </div>
                    {ev.watchlist && (
                      <span className="flex items-center gap-1 rounded-m3-sm bg-rose-500/15 border border-rose-500/30 px-2 py-1 text-xs font-bold text-rose-500">
                        <ShieldAlert className="size-3.5" /> ALERTA
                      </span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            /* Standard Matrix Mode (1, 4, 9, 16) */
            <div
              className={cn(
                "grid h-full w-full gap-1 p-1 overflow-hidden",
                layoutCount === 1 && "grid-cols-1 grid-rows-1",
                layoutCount === 4 && "grid-cols-2 grid-rows-2",
                layoutCount === 9 && "grid-cols-3 grid-rows-3",
                layoutCount === 16 && "grid-cols-4 grid-rows-4",
              )}
            >
              {Array.from({ length: layoutCount }).map((_, i) => {
                const ev = events[i];
                return (
                  <div key={i} className="relative rounded-m3-md overflow-hidden bg-black/70 border border-line/50">
                    {ev ? (
                      <SlotRenderer
                        event={ev}
                        slotIndex={i}
                        pinned={Boolean(pinnedSlots[i])}
                        onTogglePin={() => togglePin(i)}
                        debug={debugMode}
                        quality={layoutCount === 1 ? "main" : "sub"}
                        onPlayback={(id) => void navigate({ to: "/playback", search: { camera: id } })}
                      />
                    ) : (
                      <div className="flex h-full flex-col items-center justify-center text-muted text-xs gap-1">
                        <Eye className="size-5 opacity-40" />
                        <span>Sin evento asignado</span>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Collapsible Active Events Sidebar */}
        {sidebarOpen && (
          <aside
            data-testid="vision-events-sidebar"
            className="w-80 shrink-0 border border-line bg-surface rounded-m3-lg flex flex-col min-h-0 overflow-hidden"
          >
            <div className="p-3 border-b border-line flex items-center justify-between">
              <div className="font-semibold text-sm text-ink flex items-center gap-2">
                <Layers className="size-4 text-primary" />
                <span>Eventos Activos ({events.length})</span>
              </div>
              <IconButton icon={X} title="Cerrar panel" aria-label="Cerrar panel" size="sm" onClick={() => setSidebarOpen(false)} />
            </div>

            <div className="flex-1 overflow-y-auto divide-y divide-line/40">
              {events.map((ev, i) => (
                <div
                  key={ev.id}
                  onClick={() => {
                    setFocusHeroIndex(i);
                    setMode("focus");
                  }}
                  className="p-3 hover:bg-surface-2 transition-colors cursor-pointer flex flex-col gap-1.5"
                >
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-xs text-ink truncate">{ev.cameraName}</span>
                    <span className="font-mono text-xs font-bold text-primary bg-primary/10 px-1.5 py-0.5 rounded-m3-sm">
                      Score {ev.score}
                    </span>
                  </div>
                  <div className="flex items-center justify-between text-[11px] text-muted">
                    <span className="uppercase font-medium text-emerald-600">{ev.object} ({Math.round(ev.confidence * 100)}%)</span>
                    <span>{ev.zone}</span>
                  </div>
                  {ev.plate && (
                    <div className="font-mono text-xs font-bold tracking-widest text-ink bg-surface-3 px-2 py-0.5 rounded-m3-sm self-start">
                      {ev.plate}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </aside>
        )}
      </div>
    </div>
  );
}

function SlotRenderer({
  event,
  pinned,
  onTogglePin,
  debug,
  quality,
  onPlayback,
}: {
  event: VisionEventRecord;
  slotIndex?: number;
  pinned: boolean;
  onTogglePin: () => void;
  debug: boolean;
  quality: "sub" | "main";
  compact?: boolean;
  onPlayback?: (id: string) => void;
}) {
  return (
    <div className="group relative w-full h-full flex flex-col justify-between">
      {/* Video layer */}
      <div className="absolute inset-0">
        <MsePlayer cameraId={event.cameraId} quality={quality} />
      </div>

      {/* Top HUD Controls */}
      <div className="relative z-10 flex items-center justify-between p-2 bg-gradient-to-b from-black/80 via-black/30 to-transparent">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="font-semibold text-xs text-white drop-shadow-xs truncate">{event.cameraName}</span>
          {pinned && (
            <span className="flex items-center gap-0.5 bg-amber-500/90 text-black px-1 py-0.2 text-[9px] font-bold rounded-m3-xs">
              PIN
            </span>
          )}
        </div>

        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
          <IconButton
            icon={pinned ? Pin : PinOff}
            title={pinned ? "Fijada (clic para desfijar)" : "Fijar cámara"}
            aria-label={pinned ? "Fijada" : "Fijar cámara"}
            aria-pressed={pinned}
            size="sm"
            onClick={(e) => {
              e.stopPropagation();
              onTogglePin();
            }}
            className={cn("size-6 bg-black/60 text-white hover:bg-black/80", pinned && "text-amber-400")}
          />
          {onPlayback && (
            <IconButton
              icon={History}
              title="Abrir en Grabaciones"
              aria-label="Abrir en Grabaciones"
              size="sm"
              onClick={(e) => {
                e.stopPropagation();
                onPlayback(event.cameraId);
              }}
              className="size-6 bg-black/60 text-white hover:bg-black/80"
            />
          )}
        </div>
      </div>

      {/* Debug Score Mathematical Breakdown */}
      {debug && (
        <div data-testid="vision-debug-overlay" className="relative z-10 mx-2 p-1.5 rounded-m3-sm bg-black/90 text-[10px] font-mono text-emerald-400 border border-emerald-500/40 space-y-0.5">
          <div className="font-bold text-white">Score: {event.score}</div>
          <div className="flex flex-wrap gap-x-2 text-muted">
            {Object.entries(event.breakdown).map(([k, v]) => (
              <span key={k}>
                {k}: {v > 0 ? `+${v}` : v}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Bottom HUD Information */}
      <div className="relative z-10 flex items-center justify-between p-2 bg-gradient-to-t from-black/85 via-black/40 to-transparent text-white text-xs">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="font-bold uppercase tracking-wider text-[10px] bg-emerald-500/80 px-1.5 py-0.5 rounded-m3-xs text-black">
            {event.object} {Math.round(event.confidence * 100)}%
          </span>
          <span className="text-[11px] text-white/80 truncate">{event.zone}</span>
        </div>

        <div className="font-mono text-[10px] text-white/70">
          00:{String(event.durationSeconds).padStart(2, "0")}
        </div>
      </div>
    </div>
  );
}
