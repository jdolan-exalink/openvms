import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  Eye, Pin, PinOff, Maximize2, Minimize2, History, LayoutGrid, Sparkles,
  CarFront, Bell, Check, X, Bug, Layers, RefreshCw, Scan,
} from "lucide-react";
import { MsePlayer } from "@/components/MsePlayer";
import { Button, IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { useVisionSlots, type VisionAILayout, type VisionAISlot, type VisionAISource } from "@/lib/visionAi";

export type VisionMode = "matrix" | "focus" | "lpr" | "alarm";

/** Matrix mode with LPR/alarm sources lists the best candidates, so it asks for a wide grid. */
const LPR_LAYOUT: VisionAILayout = "9";

function sourceFor(mode: VisionMode, preset: VisionAISource): VisionAISource {
  if (mode === "lpr") return "lpr";
  if (mode === "alarm") return "alarmas";
  return preset;
}

function layoutFor(mode: VisionMode, layoutCount: number): VisionAILayout {
  if (mode === "focus") return "focus";
  if (mode === "lpr") return LPR_LAYOUT;
  return String(layoutCount) as VisionAILayout;
}

export function VisionAI() {
  const navigate = useNavigate();

  // State
  const [mode, setMode] = useState<VisionMode>("matrix");
  const [presetSource, setPresetSource] = useState<VisionAISource>("general");
  const [layoutCount, setLayoutCount] = useState<number>(4);
  const [fullscreen, setFullscreen] = useState(false);
  const [debugMode, setDebugMode] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [focusHeroIndex, setFocusHeroIndex] = useState<number>(0);

  const source = sourceFor(mode, presetSource);
  const layout = layoutFor(mode, layoutCount);
  const slotsQuery = useVisionSlots(source, layout);

  // Pins are client side: they freeze the slot's current assignment. Slot numbers only mean the
  // same thing inside one source+layout, so pins are scoped to that key.
  const pinKey = `${source}:${layout}`;
  const [pinState, setPinState] = useState<{ key: string; pins: Record<number, VisionAISlot> }>({ key: pinKey, pins: {} });
  const pins = pinState.key === pinKey ? pinState.pins : {};

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
        setPresetSource("personas");
        setMode("matrix");
        break;
      case "vehiculos":
        setActiveObjects({ person: false, car: true, motorcycle: true, truck: true, license_plate: true, face: false });
        setPresetSource("vehiculos");
        setMode("matrix");
        break;
      case "lpr":
        setActiveObjects({ person: false, car: true, motorcycle: false, truck: true, license_plate: true, face: false });
        setPresetSource("lpr");
        setMode("lpr");
        break;
      case "perimetro":
        setActiveObjects({ person: true, car: true, motorcycle: true, truck: true, license_plate: true, face: true });
        setPresetSource("perimetro");
        setMode("matrix");
        break;
      case "alarmas":
        setPresetSource("alarmas");
        setMode("alarm");
        break;
      case "todo":
      default:
        setActiveObjects({ person: true, car: true, motorcycle: true, truck: true, license_plate: true, face: true });
        setPresetSource("general");
        setMode("matrix");
        break;
    }
  };

  // Ranked slots from the server, with the client-side object chips and pins applied. The chips
  // only narrow the matrix/focus views; LPR and alarm modes already come filtered by source.
  const serverSlots = slotsQuery.data?.slots ?? [];
  const chipsApply = mode === "matrix" || mode === "focus";
  const slots: VisionAISlot[] = serverSlots.map((sl) => {
    const pinned = pins[sl.slot];
    if (pinned) return { ...pinned, slot: sl.slot, pinned: true };
    if (sl.camera_id && chipsApply && !(activeObjects[sl.object ?? ""] ?? true)) {
      return { slot: sl.slot, camera_id: null, pinned: false };
    }
    return sl;
  });
  const filled = slots.filter((sl) => sl.camera_id);
  const lprSlots = filled.filter((sl) => sl.plate);
  const hasAnyCamera = serverSlots.some((sl) => sl.camera_id) || Object.keys(pins).length > 0;

  const togglePin = (slotIndex: number) => {
    setPinState(() => {
      const next = { ...pins };
      const current = slots.find((sl) => sl.slot === slotIndex);
      if (next[slotIndex]) delete next[slotIndex];
      else if (current?.camera_id) next[slotIndex] = current;
      return { key: pinKey, pins: next };
    });
  };

  const toggleFilter = (obj: string) => {
    setActiveObjects((prev) => ({ ...prev, [obj]: !prev[obj] }));
  };

  const openPlayback = (id: string) => void navigate({ to: "/playback", search: { camera: id } });
  const renderSlot = (sl: VisionAISlot, quality: "sub" | "main", compact?: boolean) => (
    <SlotRenderer
      slot={sl}
      pinned={Boolean(pins[sl.slot])}
      onTogglePin={() => togglePin(sl.slot)}
      debug={debugMode}
      quality={quality}
      compact={compact}
      onPlayback={openPlayback}
    />
  );
  const emptySlot = (
    <div className="flex h-full flex-col items-center justify-center text-muted text-xs gap-1">
      <Eye className="size-5 opacity-40" />
      <span>Sin evento asignado</span>
    </div>
  );

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
            <Button size="sm" variant="tonal" onClick={() => applyPreset("perimetro")}>Perímetro</Button>
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
              {filled.length}
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
          {slotsQuery.isPending ? (
            <div role="status" className="flex h-full items-center justify-center text-muted text-sm">Cargando eventos...</div>
          ) : slotsQuery.isError && !slotsQuery.data ? (
            <div role="alert" className="flex h-full flex-col items-center justify-center gap-3 text-sm text-muted">
              <span>{slotsQuery.error instanceof Error ? slotsQuery.error.message : "No se pudieron cargar los eventos"}</span>
              <Button size="sm" variant="tonal" onClick={() => void slotsQuery.refetch()}>
                <RefreshCw className="size-3.5" />
                Reintentar
              </Button>
            </div>
          ) : !hasAnyCamera ? (
            <div className="flex h-full flex-col items-center justify-center gap-1 text-muted text-sm">
              <Eye className="size-6 opacity-40" />
              <span>Sin eventos recientes</span>
              <span className="text-xs">Las cámaras aparecerán aquí cuando haya actividad.</span>
            </div>
          ) : mode === "focus" ? (
            /* Focus Mode: 1 Hero Slot + Strip of Thumbnails */
            <div className="flex flex-col h-full min-h-0 gap-1.5 p-1.5">
              <div data-testid="vision-hero-slot" className="flex-1 min-h-0 relative rounded-m3-md overflow-hidden bg-black/60 border border-line/60">
                {slots[focusHeroIndex]?.camera_id ? (
                  renderSlot(slots[focusHeroIndex]!, "main")
                ) : (
                  <div className="flex h-full items-center justify-center text-muted text-sm">Esperando evento prioritario...</div>
                )}
              </div>

              {/* Sub-strip of the remaining slots */}
              <div data-testid="vision-sub-strip" className="h-32 shrink-0 grid grid-cols-4 gap-1.5">
                {slots.filter((sl) => sl.slot !== focusHeroIndex).slice(0, 4).map((sl) => (
                  <div
                    key={sl.slot}
                    onClick={() => sl.camera_id && setFocusHeroIndex(sl.slot)}
                    className={cn(
                      "relative rounded-m3-md overflow-hidden bg-black/40 border border-line cursor-pointer hover:border-primary transition-colors",
                    )}
                  >
                    {sl.camera_id ? renderSlot(sl, "sub", true) : emptySlot}
                  </div>
                ))}
              </div>
            </div>
          ) : mode === "lpr" ? (
            /* Specialized LPR Mode */
            lprSlots.length === 0 ? (
              <div data-testid="vision-lpr-empty" className="flex h-full flex-col items-center justify-center gap-1 text-muted text-sm">
                <Scan className="size-6 opacity-40" />
                {filled.length > 0 ? (
                  <>
                    <span>Lecturas de patente no disponibles</span>
                    <span className="text-xs">No hay patentes visibles para tu usuario (se requiere permiso de LPR).</span>
                  </>
                ) : (
                  <span>Sin lecturas de patente recientes</span>
                )}
              </div>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2 p-2 h-full overflow-y-auto">
                {lprSlots.map((sl) => (
                  <div key={sl.slot} className="flex flex-col rounded-m3-lg bg-surface border border-line overflow-hidden shadow-xs">
                    <div className="relative aspect-video bg-black/80">{renderSlot(sl, "main")}</div>
                    <div data-testid="vision-lpr-hud" className="p-3 bg-surface-2 flex items-center justify-between gap-3 border-t border-line">
                      <div>
                        <div className="text-[10px] uppercase font-mono tracking-wider text-muted">Patente Detectada</div>
                        <div className="text-xl font-bold font-mono text-ink tracking-widest">{sl.plate}</div>
                      </div>
                      <div className="text-right">
                        <div className="text-xs font-semibold text-primary">{sl.camera_name}</div>
                        {sl.confidence !== undefined && (
                          <div className="text-[10px] text-muted">Confianza: {Math.round(sl.confidence * 100)}%</div>
                        )}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )
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
                const sl = slots[i];
                return (
                  <div key={i} className="relative rounded-m3-md overflow-hidden bg-black/70 border border-line/50">
                    {sl?.camera_id ? renderSlot(sl, layoutCount === 1 ? "main" : "sub") : emptySlot}
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
                <span>Eventos Activos ({filled.length})</span>
              </div>
              <IconButton icon={X} title="Cerrar panel" aria-label="Cerrar panel" size="sm" onClick={() => setSidebarOpen(false)} />
            </div>

            <div className="flex-1 overflow-y-auto divide-y divide-line/40">
              {filled.map((sl) => (
                <div
                  key={sl.slot}
                  onClick={() => {
                    // Freeze this event as the hero of the focus view.
                    setPinState({ key: `${sourceFor("focus", presetSource)}:focus`, pins: { 0: sl } });
                    setFocusHeroIndex(0);
                    setMode("focus");
                  }}
                  className="p-3 hover:bg-surface-2 transition-colors cursor-pointer flex flex-col gap-1.5"
                >
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-xs text-ink truncate">{sl.camera_name}</span>
                    {sl.score !== undefined && (
                      <span className="font-mono text-xs font-bold text-primary bg-primary/10 px-1.5 py-0.5 rounded-m3-sm">
                        Score {Math.round(sl.score)}
                      </span>
                    )}
                  </div>
                  <div className="flex items-center justify-between text-[11px] text-muted">
                    <span className="uppercase font-medium text-emerald-600">
                      {sl.object}
                      {sl.confidence !== undefined && ` (${Math.round(sl.confidence * 100)}%)`}
                    </span>
                    <span>{sl.zone}</span>
                  </div>
                  {sl.plate && (
                    <div className="font-mono text-xs font-bold tracking-widest text-ink bg-surface-3 px-2 py-0.5 rounded-m3-sm self-start">
                      {sl.plate}
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
  slot,
  pinned,
  onTogglePin,
  debug,
  quality,
  onPlayback,
}: {
  slot: VisionAISlot;
  pinned: boolean;
  onTogglePin: () => void;
  debug: boolean;
  quality: "sub" | "main";
  compact?: boolean;
  onPlayback?: (id: string) => void;
}) {
  const cameraId = slot.camera_id;
  if (!cameraId) return null;
  return (
    <div className="group relative w-full h-full flex flex-col justify-between">
      {/* Video layer: keyed by camera so a swap re-mounts the player cleanly */}
      <div className="absolute inset-0">
        <MsePlayer key={cameraId} cameraId={cameraId} quality={quality} objectFit="fill" className="size-full" />
      </div>

      {/* Top HUD Controls */}
      <div className="relative z-10 flex items-center justify-between p-2 bg-gradient-to-b from-black/80 via-black/30 to-transparent">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="font-semibold text-xs text-white drop-shadow-xs truncate">{slot.camera_name}</span>
          {pinned && (
            <span className="flex items-center gap-0.5 bg-amber-500/90 text-black px-1 py-0.2 text-[9px] font-bold rounded-m3-xs">
              PIN
            </span>
          )}
          {slot.alarm && (
            <span className="bg-bad/80 text-white px-1 text-[9px] font-bold uppercase rounded-m3-xs">{slot.alarm}</span>
          )}
        </div>

        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity">
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
                onPlayback(cameraId);
              }}
              className="size-6 bg-black/60 text-white hover:bg-black/80"
            />
          )}
        </div>
      </div>

      {/* Debug Score Mathematical Breakdown */}
      {debug && (
        <div data-testid="vision-debug-overlay" className="relative z-10 mx-2 p-1.5 rounded-m3-sm bg-black/90 text-[10px] font-mono text-emerald-400 border border-emerald-500/40 space-y-0.5">
          <div className="font-bold text-white">Score: {Math.round(slot.score ?? 0)}</div>
          <div className="flex flex-wrap gap-x-2 text-muted">
            {Object.entries(slot.breakdown ?? {}).map(([k, v]) => (
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
          {slot.object && (
            <span className="font-bold uppercase tracking-wider text-[10px] bg-emerald-500/80 px-1.5 py-0.5 rounded-m3-xs text-black">
              {slot.object}
              {slot.confidence !== undefined && ` ${Math.round(slot.confidence * 100)}%`}
            </span>
          )}
          {slot.plate && (
            <span className="font-mono font-bold tracking-widest text-[10px] bg-white/90 px-1.5 py-0.5 rounded-m3-xs text-black">
              {slot.plate}
            </span>
          )}
          {slot.zone && <span className="text-[11px] text-white/80 truncate">{slot.zone}</span>}
        </div>

        {slot.display_seconds !== undefined && (
          <div className="font-mono text-[10px] text-white/70">{formatElapsed(slot.display_seconds)}</div>
        )}
      </div>
    </div>
  );
}

function formatElapsed(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  return `${String(Math.floor(total / 60)).padStart(2, "0")}:${String(total % 60).padStart(2, "0")}`;
}
