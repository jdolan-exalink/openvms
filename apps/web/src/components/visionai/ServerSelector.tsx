import { Server } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import type { ServerOption } from "@/lib/serverSelector";

/**
 * Drop-down with a check per server to choose which servers' events Vision AI shows. The list stays
 * open so several servers can be changed in one go, and it closes with Escape or a click outside.
 * The last selected server cannot be unchecked (nothing would be left to show). With a single
 * server there is nothing to choose, so it draws nothing.
 */
export function ServerSelector({ options, onToggle }: { options: ServerOption[]; onToggle: (serverId: string, checked: boolean) => void }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [box, setBox] = useState<{ top: number; left: number } | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const menuId = useId();

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: MouseEvent) => {
      const target = event.target as Node;
      if (rootRef.current?.contains(target) || menuRef.current?.contains(target)) return;
      setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  if (options.length < 2) return null;

  const toggleMenu = () => {
    if (open) {
      setOpen(false);
      return;
    }
    const rect = rootRef.current?.getBoundingClientRect();
    if (rect) setBox({ top: rect.bottom + 4, left: rect.left });
    setOpen(true);
  };

  const checkedCount = options.filter((o) => o.checked).length;
  const lastChecked = checkedCount === 1;
  const sites = [...new Set(options.map((o) => o.siteName))];

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-haspopup="true"
        aria-expanded={open}
        aria-controls={menuId}
        onClick={toggleMenu}
        className={cn(
          "inline-flex h-8 shrink-0 items-center gap-1.5 rounded border border-line bg-surface px-2 text-xs font-medium hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent",
          open && "border-accent",
        )}
      >
        <Server aria-hidden="true" className="size-3.5" />
        <span>{t("live.serverSelector")}</span>
        <span className="font-mono tabular-nums text-on-surface-variant">{checkedCount}/{options.length}</span>
      </button>
      {open && box && createPortal(
        <div
          ref={menuRef}
          id={menuId}
          role="group"
          aria-label={t("live.serversSelected")}
          className="fixed z-[70] max-h-80 w-64 overflow-auto rounded border border-line bg-surface py-1 shadow-lg"
          style={{ top: box.top, left: box.left }}
        >
          {sites.map((site) => (
            <div key={site}>
              {sites.length > 1 && <div className="px-2 pb-0.5 pt-1.5 text-[10px] font-bold uppercase tracking-wider text-on-surface-variant">{site}</div>}
              {options
                .filter((o) => o.siteName === site)
                .map((o) => (
                  <label key={o.id} className={cn("flex items-center gap-2 px-2 py-1.5 text-sm hover:bg-raised", o.checked && lastChecked ? "cursor-not-allowed opacity-70" : "cursor-pointer")}>
                    <input
                      type="checkbox"
                      checked={o.checked}
                      disabled={o.checked && lastChecked}
                      onChange={(event) => {
                        // Disabled already blocks a real click; this keeps the rule if the event arrives anyway.
                        if (o.checked && lastChecked) return;
                        onToggle(o.id, event.target.checked);
                      }}
                      className="size-4 shrink-0 accent-primary"
                    />
                    <span className="min-w-0 flex-1 truncate">{o.name}</span>
                    <span title={t("live.serverCameras")} className="shrink-0 font-mono text-[11px] tabular-nums text-on-surface-variant">{o.cameras}</span>
                  </label>
                ))}
            </div>
          ))}
        </div>,
        document.body,
      )}
    </div>
  );
}
