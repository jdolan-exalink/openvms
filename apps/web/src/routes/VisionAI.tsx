import { startTransition, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  Eye, Pin, PinOff, Maximize2, Minimize2, History, LayoutGrid, Sparkles,
  CarFront, Bell, Check, X, Bug, Layers, RefreshCw, Scan, LayoutDashboard,
} from "lucide-react";
import { meQuery } from "@/api/queries";
import { MsePlayer } from "@/components/MsePlayer";
import { Button, IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { can } from "@/lib/perm";
import { dynamicGridShape, priorityOrder } from "@/lib/visionAiGrid";
import { ObjectIcon, PresetIcon } from "@/lib/visionAiIcons";
import { DEFAULT_VISION_PREFS, loadVisionPrefs, saveVisionPrefs, VISION_INACTIVITY_OPTIONS, visionPrefsKey, type VisionMode } from "@/lib/visionAiPrefs";
import { detectionAgeSeconds, formatDetectionAge, useVisionSlots, type VisionAILayout, type VisionAISlot, type VisionAISource } from "@/lib/visionAi";

export type { VisionMode };

/** Matrix mode with LPR/alarm sources lists the best candidates, so it asks for a wide grid. */
const LPR_LAYOUT: VisionAILayout = "9";

/** Dynamic mode asks for every slot and renders only the filled ones. */
const DYNAMIC_LAYOUT: VisionAILayout = "16";
const DYNAMIC_MAX_TILES = 16;

function sourceFor(mode: VisionMode, preset: VisionAISource): VisionAISource {
  if (mode === "lpr") return "lpr";
  if (mode === "faces") return "personas";
  if (mode === "alarm") return "alarmas";
  return preset;
}

function layoutFor(mode: VisionMode, layoutCount: number): VisionAILayout {
  if (mode === "focus") return "focus";
  if (mode === "lpr" || mode === "faces") return LPR_LAYOUT;
  if (mode === "dynamic") return DYNAMIC_LAYOUT;
  return String(layoutCount) as VisionAILayout;
}

export function VisionAI() {
  const navigate = useNavigate();

  // State
  const [mode, setMode] = useState<VisionMode>(DEFAULT_VISION_PREFS.mode);
  const [presetSource, setPresetSource] = useState<VisionAISource>(DEFAULT_VISION_PREFS.presetSource);
  const [layoutCount, setLayoutCount] = useState<number>(DEFAULT_VISION_PREFS.layoutCount);
  const [inactivitySeconds, setInactivitySeconds] = useState<number>(DEFAULT_VISION_PREFS.inactivitySeconds);
  const [fullscreen, setFullscreen] = useState(false);
  const [debugMode, setDebugMode] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [focusHeroIndex, setFocusHeroIndex] = useState<number>(0);

  const source = sourceFor(mode, presetSource);
  const layout = layoutFor(mode, layoutCount);
  const slotsQuery = useVisionSlots(source, layout, inactivitySeconds);
  const me = useQuery(meQuery);
  const canViewSnapshots = can(me.data, "snapshots.view");

  // Pins are client side: they freeze the slot's current assignment. Slot numbers only mean the
  // same thing inside one source+layout, so pins are scoped to that key.
  const pinKey = `${source}:${layout}`;
  const [pinState, setPinState] = useState<{ key: string; pins: Record<number, VisionAISlot> }>({ key: pinKey, pins: {} });
  const pins = pinState.key === pinKey ? pinState.pins : {};

  // Active object filters
  const [activeObjects, setActiveObjects] = useState<Record<string, boolean>>({ ...DEFAULT_VISION_PREFS.activeObjects });

  // The view is remembered per tenant+user. Nothing is read or written until `me` is known; the
  // saved view is applied once per user (state adjusted during render, before children see it).
  const prefsKey = me.data ? visionPrefsKey(me.data.tenant_id ?? null, me.data.id) : null;
  const [hydratedKey, setHydratedKey] = useState<string | null>(null);
  if (prefsKey && hydratedKey !== prefsKey) {
    setHydratedKey(prefsKey);
    // On the first load, a change made while `me` was still loading wins over the saved view. When
    // the signed-in user or tenant changes later, always load that user's own view.
    const firstLoad = hydratedKey === null;
    const untouched = JSON.stringify({ mode, presetSource, layoutCount, activeObjects, inactivitySeconds }) === JSON.stringify(DEFAULT_VISION_PREFS);
    if (!firstLoad || untouched) {
      const saved = loadVisionPrefs(prefsKey);
      setMode(saved.mode);
      setPresetSource(saved.presetSource);
      setLayoutCount(saved.layoutCount);
      setActiveObjects(saved.activeObjects);
      setInactivitySeconds(saved.inactivitySeconds);
    }
  }
  useEffect(() => {
    if (prefsKey && hydratedKey === prefsKey) saveVisionPrefs(prefsKey, { mode, presetSource, layoutCount, activeObjects, inactivitySeconds });
  }, [prefsKey, hydratedKey, mode, presetSource, layoutCount, activeObjects, inactivitySeconds]);

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
  // only narrow the matrix/focus views; LPR, face and alarm modes already come filtered by source.
  const serverSlots = slotsQuery.data?.slots ?? [];
  const chipsApply = mode === "matrix" || mode === "focus" || mode === "dynamic";
  const slots: VisionAISlot[] = serverSlots.map((sl) => {
    const pinned = pins[sl.slot];
    if (pinned) return { ...pinned, slot: sl.slot, pinned: true };
    if (sl.camera_id && chipsApply && !(activeObjects[sl.object ?? ""] ?? true)) {
      return { slot: sl.slot, camera_id: null, pinned: false };
    }
    return sl;
  });
  const filled = slots.filter((sl) => sl.camera_id);
  // Dynamic tiles are keyed by camera: a pinned camera may also hold a ranked slot, so keep its first
  // occurrence only, and never draw past the 4x4 grid the shape is capped to.
  const dynamicTiles = filled.filter((sl, i) => filled.findIndex((o) => o.camera_id === sl.camera_id) === i).slice(0, DYNAMIC_MAX_TILES);
  const dynamicShape = dynamicGridShape(dynamicTiles.length);
  // Highest priority first; applied as CSS order so swapping places never re-mounts a player.
  const matrixOrder = priorityOrder(slots.slice(0, layoutCount));
  const dynamicOrder = priorityOrder(dynamicTiles);
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
            {[
              { key: "todo", label: "Todo" },
              { key: "personas", label: "Personas" },
              { key: "vehiculos", label: "Vehículos" },
              { key: "lpr", label: "LPR" },
              { key: "perimetro", label: "Perímetro" },
              { key: "alarmas", label: "Alarmas" },
            ].map(({ key, label }) => {
              return (
                <Button key={key} size="sm" variant="tonal" onClick={() => applyPreset(key)} className="flex items-center gap-1">
                  <PresetIcon preset={key} className="size-3.5" />
                  {label}
                </Button>
              );
            })}
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
                  <ObjectIcon object={key} className="size-3.5" />
                  {label} {active && <Check aria-hidden="true" className="size-3" />}
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
              onClick={() => setMode("dynamic")}
              className={cn("flex items-center gap-1 px-2 py-1 rounded-m3-sm text-xs font-medium transition-colors", mode === "dynamic" && "bg-surface text-primary shadow-xs")}
            >
              <LayoutDashboard className="size-3.5" />
              <span>Dinámica</span>
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
              onClick={() => setMode("faces")}
              className={cn("flex items-center gap-1 px-2 py-1 rounded-m3-sm text-xs font-medium transition-colors", mode === "faces" && "bg-surface text-primary shadow-xs")}
            >
              <Scan className="size-3.5" />
              <span>Rostros</span>
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

          {/* Matrix size selectors (not applicable to the dynamic grid) */}
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

          {/* Birdseye-style inactivity timeout */}
          <label className="flex items-center gap-1.5 text-xs text-on-surface-variant">
            <span className="hidden lg:inline">Cerrar tras</span>
            <select
              aria-label="Cerrar tras inactividad"
              value={inactivitySeconds}
              onChange={(e) => setInactivitySeconds(Number(e.target.value))}
              className="h-8 rounded-m3-md border border-line bg-surface-2 px-2 text-xs font-medium text-ink"
            >
              {VISION_INACTIVITY_OPTIONS.map((n) => (
                <option key={n} value={n}>{inactivityLabel(n)}</option>
              ))}
            </select>
          </label>

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
          ) : mode === "faces" ? (
            <FaceReadingsList
              slots={slotsQuery.isPlaceholderData ? [] : serverSlots.filter((slot) => slot.object === "face")}
              snapshots={canViewSnapshots}
            />
          ) : !hasAnyCamera || (mode === "dynamic" && dynamicTiles.length === 0) ? (
            <div className="flex h-full flex-col items-center justify-center gap-1 text-muted text-sm">
              <Eye className="size-6 opacity-40" />
              <span>Sin eventos recientes</span>
              <span className="text-xs">Las cámaras aparecerán aquí cuando haya actividad.</span>
            </div>
          ) : mode === "dynamic" ? (
            /* Dynamic Mode: only filled slots, grid sized to the active count */
            <div
              data-testid="vision-dynamic-grid"
              className="grid h-full w-full gap-1 p-1 overflow-hidden"
              style={{
                gridTemplateColumns: `repeat(${dynamicShape.cols}, minmax(0, 1fr))`,
                gridTemplateRows: `repeat(${dynamicShape.rows}, minmax(0, 1fr))`,
              }}
            >
              {dynamicTiles.map((sl) => (
                <div
                  key={sl.camera_id}
                  data-vision-tile
                  style={{ order: dynamicOrder.get(sl.slot) }}
                  className="relative min-h-0 rounded-m3-md overflow-hidden bg-black/70 border border-line/50"
                >
                  {renderSlot(sl, dynamicTiles.length === 1 ? "main" : "sub")}
                </div>
              ))}
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
            <LprReadingsList
              slots={slotsQuery.isPlaceholderData ? [] : lprSlots}
              platesVisible={slotsQuery.data?.plates_visible ?? true}
            />
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
                  <div
                    key={i}
                    data-vision-tile
                    style={{ order: sl ? matrixOrder.get(sl.slot) : layoutCount }}
                    className="relative rounded-m3-md overflow-hidden bg-black/70 border border-line/50"
                  >
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
                  <EventSnapshot slot={sl} snapshots={canViewSnapshots} />
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-xs text-ink truncate">{sl.camera_name}</span>
                    {sl.score !== undefined && (
                      <span className="font-mono text-xs font-bold text-primary bg-primary/10 px-1.5 py-0.5 rounded-m3-sm">
                        Score {Math.round(sl.score)}
                      </span>
                    )}
                  </div>
                  <div className="flex items-center justify-between text-[11px] text-muted">
                    <span className="inline-flex items-center gap-1 uppercase font-medium text-emerald-600">
                      <ObjectIcon object={sl.object} className="size-3" />
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

type FaceReading = {
  key: string;
  slot: VisionAISlot;
};

const MAX_FACE_READINGS = 30;

function FaceReadingsList({ slots, snapshots }: { slots: VisionAISlot[]; snapshots: boolean }) {
  const [readings, setReadings] = useState<FaceReading[]>([]);

  useEffect(() => {
    const incoming = slots.flatMap((slot) => {
      if (slot.object !== "face" || !slot.event_id || !slot.camera_id) return [];
      return [{ key: slot.event_id, slot }];
    });
    if (incoming.length === 0) return;

    startTransition(() => {
      setReadings((current) => {
        const seen = new Set(current.map((reading) => reading.key));
        const fresh = incoming.filter((reading) => {
          if (seen.has(reading.key)) return false;
          seen.add(reading.key);
          return true;
        });
        if (fresh.length === 0) return current;
        return [...fresh, ...current]
          .sort((a, b) => lprTimestamp(b.slot.assigned_at) - lprTimestamp(a.slot.assigned_at))
          .slice(0, MAX_FACE_READINGS);
      });
    });
  }, [slots]);

  if (readings.length === 0) {
    return (
      <div data-testid="vision-faces-empty" className="flex h-full flex-col items-center justify-center gap-1 text-muted text-sm">
        <Scan className="size-6 opacity-40" />
        <span>Sin rostros detectados recientemente</span>
        <span className="text-xs">Se muestran eventos de rostro en cámaras con permiso de eventos.</span>
      </div>
    );
  }

  return (
    <section data-testid="vision-faces-list" aria-label="Rostros detectados en vivo" aria-live="polite" className="flex h-full min-h-0 flex-col overflow-hidden bg-surface-dim p-3 sm:p-5">
      <header className="mb-3 flex shrink-0 items-center justify-between gap-3 rounded-m3-lg border border-primary/20 bg-surface px-4 py-3 shadow-xs">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-[10px] font-bold uppercase tracking-[0.18em] text-primary">
            <span className="size-2 rounded-full bg-emerald-500 animate-pulse" />
            Detecciones en vivo
          </div>
          <h2 className="mt-1 truncate text-sm font-semibold text-ink">Rostros detectados</h2>
        </div>
        <div className="shrink-0 rounded-m3-md bg-primary/10 px-3 py-1.5 text-right">
          <div className="font-mono text-lg font-bold tabular-nums text-primary">{readings.length.toString().padStart(2, "0")}</div>
          <div className="text-[9px] font-semibold uppercase tracking-wider text-muted">eventos recientes</div>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto rounded-m3-lg border border-line bg-surface shadow-xs">
        <div className="grid grid-cols-[minmax(90px,0.65fr)_minmax(100px,1fr)_minmax(72px,0.7fr)] gap-2 border-b border-line bg-surface-2 px-3 py-2 text-[9px] font-bold uppercase tracking-[0.16em] text-muted sm:grid-cols-[minmax(150px,0.7fr)_minmax(140px,1fr)_minmax(120px,0.7fr)_minmax(100px,0.6fr)] sm:gap-3 sm:px-4">
          <span>Detección</span>
          <span>Cámara</span>
          <span className="hidden sm:block">Zona</span>
          <span className="text-right">Hora</span>
        </div>
        <div className="divide-y divide-line/70">
          {readings.map((reading, index) => (
            <div key={reading.key} data-testid="vision-face-row" className="grid grid-cols-[minmax(90px,0.65fr)_minmax(100px,1fr)_minmax(72px,0.7fr)] items-center gap-2 px-3 py-3 transition-colors hover:bg-surface-2 sm:grid-cols-[minmax(150px,0.7fr)_minmax(140px,1fr)_minmax(120px,0.7fr)_minmax(100px,0.6fr)] sm:gap-3 sm:px-4">
              <div className="flex min-w-0 items-center gap-2.5">
                <span className="w-5 shrink-0 text-right font-mono text-[10px] tabular-nums text-muted">{(index + 1).toString().padStart(2, "0")}</span>
                <div className="w-24 shrink-0 sm:w-32">
                  <EventSnapshot slot={reading.slot} snapshots={snapshots} />
                </div>
              </div>
              <div className="min-w-0">
                <div className="truncate text-xs font-semibold text-ink">{reading.slot.camera_name ?? "Cámara"}</div>
                {reading.slot.confidence !== undefined && <div className="mt-0.5 text-[10px] text-muted">Confianza: {Math.round(reading.slot.confidence * 100)}%</div>}
              </div>
              <div className="hidden min-w-0 truncate text-xs text-muted sm:block">{reading.slot.zone || "—"}</div>
              <div className="text-right font-mono text-xs tabular-nums text-on-surface-variant">{formatLprTime(reading.slot.assigned_at)}</div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

type LprReading = {
  key: string;
  plate: string;
  cameraName: string;
  zone?: string;
  confidence?: number;
  assignedAt?: string;
};

const MAX_LPR_READINGS = 30;

function LprReadingsList({ slots, platesVisible }: { slots: VisionAISlot[]; platesVisible: boolean }) {
  const [readings, setReadings] = useState<LprReading[]>([]);

  useEffect(() => {
    const incoming = slots.flatMap((slot) => {
      if (!slot.plate || !slot.camera_id) return [];
      return [{
        key: slot.event_id ?? `${slot.camera_id}:${slot.plate}:${slot.assigned_at ?? slot.slot}`,
        plate: slot.plate,
        cameraName: slot.camera_name ?? "Cámara",
        zone: slot.zone,
        confidence: slot.confidence,
        assignedAt: slot.assigned_at ?? new Date().toISOString(),
      }];
    });
    if (incoming.length === 0) return;

    startTransition(() => {
      setReadings((current) => {
        const seen = new Set(current.map((reading) => reading.key));
        const fresh = incoming.filter((reading) => {
          if (seen.has(reading.key)) return false;
          seen.add(reading.key);
          return true;
        });
        if (fresh.length === 0) return current;
        return [...fresh, ...current]
          .sort((a, b) => lprTimestamp(b.assignedAt) - lprTimestamp(a.assignedAt))
          .slice(0, MAX_LPR_READINGS);
      });
    });
  }, [slots]);

  if (!platesVisible) {
    return (
      <div data-testid="vision-lpr-empty" className="flex h-full flex-col items-center justify-center gap-1 text-muted text-sm">
        <Scan className="size-6 opacity-40" />
        <span>Lecturas de patente no disponibles</span>
        <span className="text-xs">No hay patentes visibles para tu usuario (se requiere permiso de LPR).</span>
      </div>
    );
  }

  if (readings.length === 0) {
    return (
      <div data-testid="vision-lpr-empty" className="flex h-full flex-col items-center justify-center gap-1 text-muted text-sm">
        <Scan className="size-6 opacity-40" />
        <span>Sin lecturas de patente recientes</span>
      </div>
    );
  }

  return (
    <section data-testid="vision-lpr-list" aria-label="Lecturas LPR en vivo" aria-live="polite" className="flex h-full min-h-0 flex-col overflow-hidden bg-surface-dim p-3 sm:p-5">
      <header className="mb-3 flex shrink-0 items-center justify-between gap-3 rounded-m3-lg border border-primary/20 bg-surface px-4 py-3 shadow-xs">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-[10px] font-bold uppercase tracking-[0.18em] text-primary">
            <span className="size-2 rounded-full bg-emerald-500 animate-pulse" />
            Lecturas LPR en vivo
          </div>
          <h2 className="mt-1 truncate text-sm font-semibold text-ink">Control de ingreso · Argentina</h2>
        </div>
        <div className="shrink-0 rounded-m3-md bg-primary/10 px-3 py-1.5 text-right">
          <div className="font-mono text-lg font-bold tabular-nums text-primary">{readings.length.toString().padStart(2, "0")}</div>
          <div className="text-[9px] font-semibold uppercase tracking-wider text-muted">lecturas recientes</div>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto rounded-m3-lg border border-line bg-surface shadow-xs">
        <div className="grid grid-cols-[minmax(135px,0.9fr)_minmax(80px,1fr)_minmax(72px,0.7fr)] gap-2 border-b border-line bg-surface-2 px-3 py-2 text-[9px] font-bold uppercase tracking-[0.16em] text-muted sm:grid-cols-[minmax(180px,0.8fr)_minmax(140px,1fr)_minmax(120px,0.7fr)_minmax(100px,0.6fr)] sm:gap-3 sm:px-4">
          <span>Patente Detectada</span>
          <span>Cámara</span>
          <span className="hidden sm:block">Zona</span>
          <span className="text-right">Hora</span>
        </div>
        <div className="divide-y divide-line/70">
          {readings.map((reading, index) => (
            <div
              key={reading.key}
              data-testid="vision-lpr-hud"
              className="grid grid-cols-[minmax(135px,0.9fr)_minmax(80px,1fr)_minmax(72px,0.7fr)] items-center gap-2 px-3 py-3 transition-colors hover:bg-surface-2 sm:grid-cols-[minmax(180px,0.8fr)_minmax(140px,1fr)_minmax(120px,0.7fr)_minmax(100px,0.6fr)] sm:gap-3 sm:px-4"
            >
              <div className="flex min-w-0 items-center gap-2.5">
                <span className="w-5 shrink-0 text-right font-mono text-[10px] tabular-nums text-muted">{(index + 1).toString().padStart(2, "0")}</span>
                <div className="min-w-0 overflow-hidden rounded-m3-sm border border-slate-300 bg-white shadow-xs">
                  <div className="flex items-center justify-between bg-sky-800 px-2 py-0.5 text-[8px] font-bold tracking-[0.15em] text-white">
                    <span>ARGENTINA</span>
                    <span className="font-mono">ARG</span>
                  </div>
                  <div className="truncate px-2 py-1 font-mono text-lg font-black leading-none tracking-[0.12em] text-slate-900">{reading.plate}</div>
                </div>
              </div>
              <div className="min-w-0">
                <div className="truncate text-xs font-semibold text-ink">{reading.cameraName}</div>
                {reading.confidence !== undefined && <div className="mt-0.5 text-[10px] text-muted">Confianza: {Math.round(reading.confidence * 100)}%</div>}
              </div>
              <div className="hidden min-w-0 truncate text-xs text-muted sm:block">{reading.zone || "—"}</div>
              <div className="text-right font-mono text-xs tabular-nums text-on-surface-variant">{formatLprTime(reading.assignedAt)}</div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function formatLprTime(value?: string): string {
  if (!value) return "--:--:--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--:--:--";
  return new Intl.DateTimeFormat("es-AR", { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(date);
}

function lprTimestamp(value?: string): number {
  if (!value) return 0;
  const timestamp = Date.parse(value);
  return Number.isNaN(timestamp) ? 0 : timestamp;
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
        <MsePlayer
          key={cameraId}
          cameraId={cameraId}
          quality={quality}
          persistent={true}
          surface={true}
          active={true}
          suspended={false}
          objectFit="fill"
          className="size-full"
        />
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
            <span className="inline-flex items-center gap-1 font-bold uppercase tracking-wider text-[10px] bg-emerald-500/80 px-1.5 py-0.5 rounded-m3-xs text-black">
              <ObjectIcon object={slot.object} className="size-3" />
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

        {slot.detected_at ? (
          <DetectionAge detectedAt={slot.detected_at} />
        ) : slot.display_seconds !== undefined && (
          <div className="font-mono text-[10px] text-white/70">{formatElapsed(slot.display_seconds)}</div>
        )}
      </div>
    </div>
  );
}

/** Detection image of an active event: snapshot when allowed, else thumbnail, else the object icon. */
function EventSnapshot({ slot, snapshots }: { slot: VisionAISlot; snapshots: boolean }) {
  const eventId = slot.event_id;
  if (!eventId) return null;
  // Keyed by permission too: if the thumbnail failed before permissions loaded, the snapshot still gets
  // its chance once snapshots.view is known.
  return <EventSnapshotImage key={`${eventId}:${snapshots}`} slot={slot} eventId={eventId} snapshots={snapshots} />;
}

function EventSnapshotImage({ slot, eventId, snapshots }: { slot: VisionAISlot; eventId: string; snapshots: boolean }) {
  const thumbnail = `/api/v1/events/${eventId}/thumbnail`;
  // Snapshot first (when allowed), the thumbnail once on error, then the icon placeholder.
  const [stage, setStage] = useState<"primary" | "thumbnail" | "none">("primary");
  const useSnapshot = snapshots && stage === "primary";
  const src = stage === "none" ? null : useSnapshot ? `/media/v1/events/${eventId}/snapshot.jpg` : thumbnail;
  return (
    <div className="aspect-video w-full overflow-hidden rounded-m3-sm bg-surface-3">
      {src ? (
        <img
          src={src}
          alt={`Detección: ${slot.object ?? "evento"} en ${slot.camera_name ?? "cámara"}`}
          loading="lazy"
          className="size-full object-cover"
          onError={() => setStage(useSnapshot ? "thumbnail" : "none")}
        />
      ) : (
        <div data-testid={`vision-event-placeholder-${eventId}`} className="flex size-full items-center justify-center text-muted">
          <ObjectIcon object={slot.object} className="size-6 opacity-50" />
        </div>
      )}
    </div>
  );
}

function formatElapsed(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  return `${String(Math.floor(total / 60)).padStart(2, "0")}:${String(total % 60).padStart(2, "0")}`;
}

function inactivityLabel(seconds: number): string {
  if (seconds === 0) return "Nunca";
  return seconds < 60 ? `${seconds} s` : `${seconds / 60} min`;
}

/** Live "hace Ns" counter; ticks on its own so the page does not re-render every second. */
function DetectionAge({ detectedAt }: { detectedAt: string }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const seconds = detectionAgeSeconds(detectedAt, now);
  if (seconds === null) return null;
  return (
    <div className="font-mono text-[10px] font-semibold tabular-nums text-white/80" title="Tiempo desde la detección">
      {formatDetectionAge(seconds)}
    </div>
  );
}
