import { cn } from "@/lib/cn";

const WIDTH = 240;
const HEIGHT = 56;
const PAD = 2;

function points(values: readonly number[], max: number): string {
  // A single sample still draws a flat segment instead of nothing.
  const series = values.length === 1 ? [values[0]!, values[0]!] : values;
  const step = (WIDTH - PAD * 2) / Math.max(series.length - 1, 1);
  return series
    .map((v, i) => `${(PAD + i * step).toFixed(1)},${(HEIGHT - PAD - (Math.max(v, 0) / max) * (HEIGHT - PAD * 2)).toFixed(1)}`)
    .join(" ");
}

/** Tiny dependency-free line chart. `label` is the text alternative (the numbers behind the picture). */
export function Sparkline({ values, secondary, label, className }: { values: readonly number[]; secondary?: readonly number[]; label: string; className?: string }) {
  const max = Math.max(...values, 0.1);
  const secondaryMax = Math.max(...(secondary ?? []), 1);
  return (
    <svg role="img" aria-label={label} viewBox={`0 0 ${WIDTH} ${HEIGHT}`} preserveAspectRatio="none" className={cn("h-14 w-full rounded-m3-md bg-surface-2", className)}>
      {values.length > 0 && <polyline data-series="mbps" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" vectorEffect="non-scaling-stroke" className="text-primary" points={points(values, max)} />}
      {secondary && secondary.length > 0 && secondary.some((v) => v > 0) && (
        <polyline data-series="dropped" fill="none" stroke="currentColor" strokeWidth="1" strokeDasharray="3 2" vectorEffect="non-scaling-stroke" className="text-warn" points={points(secondary, secondaryMax)} />
      )}
    </svg>
  );
}
