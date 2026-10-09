import { cn } from "@/lib/cn";

/**
 * Mercosur Argentina plate, or the older black plate when the text is ABC123. Plate colors
 * model the physical plate (white/black/Mercosur blue), so they are fixed in every theme.
 */
export function ArPlate({ plate, large = false }: { plate: string; large?: boolean }) {
  const raw = plate.replace(/[^A-Za-z0-9]/g, "").toUpperCase();
  const classic = /^[A-Z]{3}\d{3}$/.test(raw);
  const shown = classic
    ? `${raw.slice(0, 3)} ${raw.slice(3)}`
    : raw.length === 7
      ? `${raw.slice(0, 2)} ${raw.slice(2, 5)} ${raw.slice(5)}`
      : plate.toUpperCase();
  return (
    <span className={cn("inline-flex overflow-hidden rounded-[4px] border shadow-sm", large ? "h-9 text-lg" : "h-6 text-[11px]", classic ? "border-neutral-900 bg-black text-white" : "border-[#0b2f6b] bg-white text-black")} aria-label={`Patente ${raw || plate}`}>
      <span className="sr-only">{raw || plate}</span>
      {!classic && (
        <span className={cn("flex flex-col items-center justify-center bg-[#003399] font-bold leading-none text-white", large ? "w-7 text-[9px]" : "w-4 text-[7px]")} aria-hidden>
          <span>★</span>
          AR
        </span>
      )}
      <span className={cn("flex items-center px-1.5 font-black tracking-[0.14em]", classic && "px-2")}>{shown}</span>
    </span>
  );
}
