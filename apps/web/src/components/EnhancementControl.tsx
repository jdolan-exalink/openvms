import { RotateCcw, Sparkles } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Icon } from "@/components/Icon";
import { cn } from "@/lib/cn";
import {
  type EnhancementAdjustments,
  type EnhancementMode,
} from "@/lib/enhancement/types";

export interface EnhancementControlProps {
  mode: EnhancementMode;
  onModeChange: (mode: EnhancementMode) => void;
  adjustments: EnhancementAdjustments;
  onAdjustmentsChange?: (adjustments: Partial<EnhancementAdjustments>) => void;
  onReset: () => void;
  className?: string;
}

export function EnhancementControl({
  mode,
  onModeChange,
  adjustments,
  onAdjustmentsChange,
  onReset,
  className,
}: EnhancementControlProps) {
  const [open, setOpen] = useState(false);
  const popoverRef = useRef<HTMLDivElement>(null);

  // Close popover on outside click
  useEffect(() => {
    if (!open) return;
    const handleClickOutside = (e: MouseEvent) => {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, [open]);

  const handleSlider = (key: keyof EnhancementAdjustments, value: number) => {
    if (mode !== "manual") {
      onModeChange("manual");
    }
    onAdjustmentsChange?.({ [key]: value });
  };

  const isAuto = mode === "auto";
  const isOff = mode === "off";
  const isManual = mode === "manual";

  return (
    <div
      ref={popoverRef}
      className={cn("relative inline-flex items-center text-xs select-none", className)}
      onPointerDown={(e) => e.stopPropagation()}
    >
      {/* Keep the enhancement affordance compact; mode selection lives in settings. */}
      <div className="flex items-center rounded-m3-md border border-white/20 bg-black/75 shadow-xs backdrop-blur-xs">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            setOpen((v) => !v);
          }}
          title="Ajustes de mejora visual"
          aria-label="Ajustes de mejora visual"
          aria-expanded={open}
          className={cn(
            "px-2 py-1 text-white/80 hover:text-white transition-colors",
            isAuto && "text-primary",
            isManual && "text-amber-400",
            isOff && "text-white/60",
          )}
        >
          <Icon icon={Sparkles} size="xs" className={cn(isAuto && "animate-pulse text-primary")} />
        </button>
      </div>

      {/* Advanced Popover Menu */}
      {open && (
        <div
          role="dialog"
          aria-label="Panel de mejora visual"
          className="absolute bottom-full left-0 mb-2 z-50 w-72 rounded-m3-lg border border-outline-variant bg-surface-1 p-3.5 shadow-xl text-ink"
        >
          <div className="flex items-center justify-between border-b border-outline-variant/60 pb-2 mb-3">
            <span className="font-semibold text-xs flex items-center gap-1.5">
              <Icon icon={Sparkles} size="xs" className="text-primary" />
              <span>Control manual de imagen</span>
            </span>
            <button
              type="button"
              onClick={() => {
                onReset();
                setOpen(false);
              }}
              title="Restablecer todos los ajustes a los valores por defecto"
              className="flex items-center gap-1 text-[11px] font-medium text-muted hover:text-ink transition-colors cursor-pointer"
            >
              <Icon icon={RotateCcw} size="xs" />
              <span>Restablecer imagen</span>
            </button>
          </div>

          {/* Mode Switcher */}
          <div className="grid grid-cols-3 gap-1 rounded-m3-md bg-surface-2 p-1 mb-3">
            <button
              type="button"
              onClick={() => onModeChange("off")}
              className={cn(
                "rounded px-2 py-1 text-center text-[11px] font-medium transition-colors",
                isOff ? "bg-surface-1 text-ink font-bold shadow-xs" : "text-muted hover:text-ink",
              )}
            >
              Off
            </button>
            <button
              type="button"
              onClick={() => onModeChange("auto")}
              className={cn(
                "rounded px-2 py-1 text-center text-[11px] font-medium transition-colors",
                isAuto ? "bg-primary text-on-primary font-bold shadow-xs" : "text-muted hover:text-ink",
              )}
            >
              Auto
            </button>
            <button
              type="button"
              onClick={() => onModeChange("manual")}
              className={cn(
                "rounded px-2 py-1 text-center text-[11px] font-medium transition-colors",
                isManual ? "bg-amber-600 text-white font-bold shadow-xs" : "text-muted hover:text-ink",
              )}
            >
              Manual
            </button>
          </div>

          {/* Sliders */}
          <div className="flex flex-col gap-2.5">
            {/* Brillo */}
            <div className="flex flex-col gap-1">
              <div className="flex justify-between text-[11px]">
                <label htmlFor="slider-brightness" className="text-muted font-medium">
                  Brillo
                </label>
                <span className="tabular-nums font-mono text-[10px]">
                  {adjustments.brightness > 0 ? `+${adjustments.brightness}` : adjustments.brightness}
                </span>
              </div>
              <input
                id="slider-brightness"
                type="range"
                min="-100"
                max="100"
                value={adjustments.brightness}
                onChange={(e) => handleSlider("brightness", Number(e.target.value))}
                className="h-1.5 w-full cursor-pointer appearance-none rounded-lg bg-surface-3 accent-primary"
              />
            </div>

            {/* Contraste */}
            <div className="flex flex-col gap-1">
              <div className="flex justify-between text-[11px]">
                <label htmlFor="slider-contrast" className="text-muted font-medium">
                  Contraste
                </label>
                <span className="tabular-nums font-mono text-[10px]">
                  {adjustments.contrast > 0 ? `+${adjustments.contrast}` : adjustments.contrast}
                </span>
              </div>
              <input
                id="slider-contrast"
                type="range"
                min="-100"
                max="100"
                value={adjustments.contrast}
                onChange={(e) => handleSlider("contrast", Number(e.target.value))}
                className="h-1.5 w-full cursor-pointer appearance-none rounded-lg bg-surface-3 accent-primary"
              />
            </div>

            {/* Gamma */}
            <div className="flex flex-col gap-1">
              <div className="flex justify-between text-[11px]">
                <label htmlFor="slider-gamma" className="text-muted font-medium">
                  Gamma
                </label>
                <span className="tabular-nums font-mono text-[10px]">
                  {adjustments.gamma.toFixed(2)}
                </span>
              </div>
              <input
                id="slider-gamma"
                type="range"
                min="0.5"
                max="2.5"
                step="0.05"
                value={adjustments.gamma}
                onChange={(e) => handleSlider("gamma", Number(e.target.value))}
                className="h-1.5 w-full cursor-pointer appearance-none rounded-lg bg-surface-3 accent-primary"
              />
            </div>

            {/* Saturación */}
            <div className="flex flex-col gap-1">
              <div className="flex justify-between text-[11px]">
                <label htmlFor="slider-saturation" className="text-muted font-medium">
                  Saturación
                </label>
                <span className="tabular-nums font-mono text-[10px]">
                  {adjustments.saturation > 0 ? `+${adjustments.saturation}` : adjustments.saturation}
                </span>
              </div>
              <input
                id="slider-saturation"
                type="range"
                min="-100"
                max="100"
                value={adjustments.saturation}
                onChange={(e) => handleSlider("saturation", Number(e.target.value))}
                className="h-1.5 w-full cursor-pointer appearance-none rounded-lg bg-surface-3 accent-primary"
              />
            </div>

            {/* Nitidez */}
            <div className="flex flex-col gap-1">
              <div className="flex justify-between text-[11px]">
                <label htmlFor="slider-sharpness" className="text-muted font-medium">
                  Nitidez
                </label>
                <span className="tabular-nums font-mono text-[10px]">
                  {adjustments.sharpness}
                </span>
              </div>
              <input
                id="slider-sharpness"
                type="range"
                min="0"
                max="100"
                value={adjustments.sharpness}
                onChange={(e) => handleSlider("sharpness", Number(e.target.value))}
                className="h-1.5 w-full cursor-pointer appearance-none rounded-lg bg-surface-3 accent-primary"
              />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
