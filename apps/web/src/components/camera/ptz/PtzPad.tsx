import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp, Minus, Plus, Square } from "lucide-react";
import { Icon } from "@/components/Icon";
import { cn } from "@/lib/cn";
import type { PtzDirection } from "./usePtz";

type Props = {
  onMove: (direction: PtzDirection) => void;
  onStop: () => void;
  disabled?: boolean;
  /** Smaller pad for the preview overlay. */
  compact?: boolean;
};

const BASE = "inline-flex items-center justify-center rounded-m3-md border border-outline-variant bg-surface-1 hover:bg-surface-3 disabled:cursor-not-allowed disabled:opacity-50";

/**
 * Pan/tilt/zoom pad. A press starts a continuous move and releasing (or leaving the button, or
 * losing focus) stops it, so a missed pointer-up can never leave the camera moving.
 */
export function PtzPad({ onMove, onStop, disabled, compact }: Props) {
  const size = compact ? "size-8" : "size-10";
  const hold = (direction: PtzDirection, label: string, icon: typeof Plus) => (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      className={cn(BASE, size)}
      onPointerDown={() => onMove(direction)}
      onPointerUp={onStop}
      onPointerLeave={(e) => e.buttons > 0 && onStop()}
      onPointerCancel={onStop}
      onKeyDown={(e) => (e.key === " " || e.key === "Enter") && !e.repeat && onMove(direction)}
      onKeyUp={(e) => (e.key === " " || e.key === "Enter") && onStop()}
    >
      <Icon icon={icon} size="xs" />
    </button>
  );
  return (
    <div className="flex items-center gap-4">
      <div role="group" aria-label="Mover" className="grid grid-cols-3 gap-1">
        <span />
        {hold("up", "Arriba", ChevronUp)}
        <span />
        {hold("left", "Izquierda", ChevronLeft)}
        <button type="button" aria-label="Detener" title="Detener" disabled={disabled} className={cn(BASE, size, "text-bad")} onClick={onStop}>
          <Icon icon={Square} size="xs" />
        </button>
        {hold("right", "Derecha", ChevronRight)}
        <span />
        {hold("down", "Abajo", ChevronDown)}
        <span />
      </div>
      <div role="group" aria-label="Zoom" className="flex flex-col gap-1">
        {hold("zoom_in", "Zoom +", Plus)}
        {hold("zoom_out", "Zoom -", Minus)}
      </div>
    </div>
  );
}
