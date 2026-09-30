import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui";
import { cn } from "@/lib/cn";

const WEEKDAYS = ["L", "M", "X", "J", "V", "S", "D"];
const MONTHS = ["enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"];

const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate());
const sameDay = (a: Date, b: Date) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();

/** monthGrid lists the cells of a Monday-first month view: null padding, then each day. */
export function monthGrid(year: number, month: number): (Date | null)[] {
  const first = new Date(year, month, 1);
  const lead = (first.getDay() + 6) % 7;
  const count = new Date(year, month + 1, 0).getDate();
  const cells: (Date | null)[] = Array(lead).fill(null);
  for (let d = 1; d <= count; d++) cells.push(new Date(year, month, d));
  while (cells.length % 7) cells.push(null);
  return cells;
}

/**
 * DayCalendar is a lightweight accessible month picker (button + popover grid). Days after
 * `max` are disabled; `marked` optionally highlights days known to have recordings.
 */
export function DayCalendar({
  value,
  max,
  marked,
  onChange,
}: {
  value: Date;
  /** Latest selectable day (usually today). */
  max: Date;
  marked?: (day: Date) => boolean;
  onChange: (day: Date) => void;
}) {
  const [open, setOpen] = useState(false);
  const [cursor, setCursor] = useState({ year: value.getFullYear(), month: value.getMonth() });
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDown = (event: PointerEvent) => {
      if (root.current && !root.current.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const shift = (delta: number) =>
    setCursor((c) => {
      const d = new Date(c.year, c.month + delta, 1);
      return { year: d.getFullYear(), month: d.getMonth() };
    });
  const limit = dayStart(max);
  const label = value.toLocaleDateString("es", { weekday: "short", day: "numeric", month: "short", year: "numeric" });

  return (
    <div ref={root} className="relative">
      <Button
        aria-label={`Elegir día, ${label}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => {
          setCursor({ year: value.getFullYear(), month: value.getMonth() });
          setOpen((o) => !o);
        }}
      >
        <CalendarDays className="size-4" aria-hidden /> {label}
      </Button>
      {open && (
        <div role="dialog" aria-label="Calendario" className="absolute bottom-full left-0 z-30 mb-1 w-64 rounded-lg border border-line bg-surface p-2 shadow-lg">
          <div className="mb-1 flex items-center justify-between">
            <Button aria-label="Mes anterior" className="px-2 py-1" onClick={() => shift(-1)}>
              <ChevronLeft className="size-4" aria-hidden />
            </Button>
            <span className="text-sm font-medium capitalize" aria-live="polite">
              {MONTHS[cursor.month]} {cursor.year}
            </span>
            <Button aria-label="Mes siguiente" className="px-2 py-1" onClick={() => shift(1)} disabled={new Date(cursor.year, cursor.month + 1, 1) > limit}>
              <ChevronRight className="size-4" aria-hidden />
            </Button>
          </div>
          <div role="grid" aria-label={`${MONTHS[cursor.month]} ${cursor.year}`} className="grid grid-cols-7 gap-0.5 text-center text-xs">
            {WEEKDAYS.map((w) => (
              <span key={w} role="columnheader" className="py-1 text-muted">
                {w}
              </span>
            ))}
            {monthGrid(cursor.year, cursor.month).map((cell, i) =>
              cell ? (
                <button
                  key={i}
                  type="button"
                  role="gridcell"
                  aria-selected={sameDay(cell, value)}
                  aria-label={cell.toLocaleDateString("es", { day: "numeric", month: "long", year: "numeric" })}
                  disabled={cell > limit}
                  onClick={() => {
                    onChange(cell);
                    setOpen(false);
                  }}
                  className={cn(
                    "rounded py-1 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-30",
                    sameDay(cell, value) && "bg-accent text-bg hover:bg-accent",
                    !sameDay(cell, value) && sameDay(cell, limit) && "border border-accent/50",
                    !sameDay(cell, value) && marked?.(cell) && "font-semibold text-accent",
                  )}
                >
                  {cell.getDate()}
                </button>
              ) : (
                <span key={i} role="gridcell" aria-hidden />
              ),
            )}
          </div>
        </div>
      )}
    </div>
  );
}
