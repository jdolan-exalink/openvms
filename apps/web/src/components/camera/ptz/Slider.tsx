import { MIN_SPEED } from "./usePtz";

/** Speed slider (fraction of the device maximum). */
export function Slider({ label, value, onChange, disabled }: { label: string; value: number; onChange: (v: number) => void; disabled?: boolean }) {
  return (
    <label className="flex items-center gap-3 text-sm">
      <span className="font-medium">{label}</span>
      <input type="range" min={MIN_SPEED} max={1} step={0.05} value={value} disabled={disabled} onChange={(e) => onChange(Number(e.target.value))} aria-valuetext={`${Math.round(value * 100)}%`} />
      <span className="font-mono text-xs text-muted">{Math.round(value * 100)}%</span>
    </label>
  );
}
