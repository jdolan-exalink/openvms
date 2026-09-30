import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import { faChevronRight } from "@fortawesome/free-solid-svg-icons";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { type KeyboardEvent, type ReactNode, type RefObject, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/cn";

export type MenuItem =
  | { separator: true; id: string }
  | {
      separator?: false;
      id: string;
      label: string;
      icon?: IconDefinition;
      /** Shown dimmed at the end of the row, e.g. "Próximamente". */
      hint?: string;
      disabled?: boolean;
      danger?: boolean;
      onSelect?: () => void;
      /** A submenu; opens on hover, click or the right arrow key. */
      children?: MenuItem[];
    };

const MARGIN = 8;

/** Clamps a box of the given size so it stays inside the viewport. */
export function clampToViewport(x: number, y: number, width: number, height: number, vw: number, vh: number) {
  return { left: Math.max(MARGIN, Math.min(x, vw - width - MARGIN)), top: Math.max(MARGIN, Math.min(y, vh - height - MARGIN)) };
}

/**
 * ContextMenu is a small accessible pointer-anchored menu (role="menu"). It renders in a portal,
 * is clamped to the viewport, supports one level of submenus and roving focus (arrows, Home/End,
 * Enter/Space activate) and closes on Escape, outside pointer, scroll, resize or selection.
 */
export function ContextMenu({ x, y, items, onClose, returnFocusTo }: { x: number; y: number; items: MenuItem[]; onClose: () => void; returnFocusTo?: RefObject<HTMLElement | null> | HTMLElement | null }) {
  const rootRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: x, top: y });

  useLayoutEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    setPos(clampToViewport(x, y, r.width, r.height, window.innerWidth, window.innerHeight));
  }, [x, y, items]);

  const closeRef = useRef(onClose);
  useEffect(() => {
    closeRef.current = onClose;
  });
  useEffect(() => {
    const close = () => closeRef.current();
    const onDown = (e: Event) => {
      if (!rootRef.current?.contains(e.target as Node)) close();
    };
    const onScroll = (e: Event) => {
      if (!rootRef.current?.contains(e.target as Node)) close();
    };
    document.addEventListener("pointerdown", onDown, true);
    document.addEventListener("contextmenu", onDown, true);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", close);
    window.addEventListener("blur", close);
    return () => {
      document.removeEventListener("pointerdown", onDown, true);
      document.removeEventListener("contextmenu", onDown, true);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", close);
      window.removeEventListener("blur", close);
    };
  }, []);

  // Give focus back to the element that opened the menu when it unmounts.
  useEffect(() => {
    const target = returnFocusTo;
    return () => {
      const el = target && "current" in target ? target.current : target;
      if (el && el.isConnected) el.focus();
    };
  }, [returnFocusTo]);

  return createPortal(
    <div ref={rootRef} style={{ left: pos.left, top: pos.top }} className="fixed z-[60]" data-context-menu="true">
      <MenuList items={items} onClose={onClose} autoFocus />
    </div>,
    document.body,
  );
}

function MenuList({ items, onClose, autoFocus, onLeave }: { items: MenuItem[]; onClose: () => void; autoFocus?: boolean; onLeave?: () => void }) {
  const listRef = useRef<HTMLDivElement>(null);
  const [openSub, setOpenSub] = useState<string | null>(null);

  const buttons = () => Array.from(listRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? []).filter((b) => b.closest('[role="menu"]') === listRef.current);
  useEffect(() => {
    if (autoFocus) buttons()[0]?.focus();
  }, [autoFocus]);

  const move = (current: HTMLElement, delta: number | "first" | "last") => {
    const all = buttons();
    if (all.length === 0) return;
    const i = all.indexOf(current as HTMLButtonElement);
    const next = delta === "first" ? 0 : delta === "last" ? all.length - 1 : (i + delta + all.length) % all.length;
    all[next]?.focus();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement;
    if (target.getAttribute("role") !== "menuitem" || target.closest('[role="menu"]') !== listRef.current) return;
    const stop = () => {
      e.preventDefault();
      e.stopPropagation();
    };
    const itemId = target.dataset.itemId ?? null;
    switch (e.key) {
      case "ArrowDown":
        stop();
        move(target, 1);
        break;
      case "ArrowUp":
        stop();
        move(target, -1);
        break;
      case "Home":
        stop();
        move(target, "first");
        break;
      case "End":
        stop();
        move(target, "last");
        break;
      case "ArrowRight":
        if (target.getAttribute("aria-haspopup") === "menu") {
          stop();
          setOpenSub(itemId);
        }
        break;
      case "ArrowLeft":
        if (onLeave) {
          stop();
          onLeave();
        }
        break;
      case "Escape":
      case "Tab":
        stop();
        onClose();
        break;
    }
  };

  return (
    <div
      ref={listRef}
      role="menu"
      onKeyDown={onKeyDown}
      className="min-w-48 max-w-72 rounded-lg border border-line bg-surface py-1 text-sm shadow-xl"
    >
      {items.map((item) => {
        if (item.separator) return <div key={item.id} role="separator" className="my-1 h-px bg-line" />;
        const hasSub = !!item.children?.length;
        const open = openSub === item.id;
        return (
          <div key={item.id} className="relative">
            <button
              type="button"
              role="menuitem"
              tabIndex={-1}
              data-item-id={item.id}
              aria-disabled={item.disabled || undefined}
              aria-haspopup={hasSub ? "menu" : undefined}
              aria-expanded={hasSub ? open : undefined}
              onMouseEnter={(e) => {
                e.currentTarget.focus();
                setOpenSub(hasSub && !item.disabled ? item.id : null);
              }}
              onClick={() => {
                if (item.disabled) return;
                if (hasSub) return setOpenSub(item.id);
                item.onSelect?.();
                onClose();
              }}
              className={cn(
                "flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-raised focus:bg-raised focus:outline-none",
                item.danger && "text-bad",
                item.disabled && "cursor-not-allowed opacity-50",
              )}
            >
              <span className="flex w-4 shrink-0 justify-center text-xs text-muted">{item.icon && <FontAwesomeIcon icon={item.icon} fixedWidth aria-hidden />}</span>
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              {item.hint && <span className="shrink-0 text-[11px] text-muted">{item.hint}</span>}
              {hasSub && <FontAwesomeIcon icon={faChevronRight} className="shrink-0 text-[10px] text-muted" aria-hidden />}
            </button>
            {hasSub && open && (
              <Submenu>
                <MenuList items={item.children ?? []} onClose={onClose} autoFocus onLeave={() => {
                  setOpenSub(null);
                  listRef.current?.querySelector<HTMLElement>(`[data-item-id="${CSS.escape(item.id)}"]`)?.focus();
                }} />
              </Submenu>
            )}
          </div>
        );
      })}
    </div>
  );
}

/** Positions a submenu beside its parent row, flipping left and/or up when it would leave the viewport. */
function Submenu({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState({ left: false, shiftUp: 0 });
  useLayoutEffect(() => {
    const r = ref.current?.getBoundingClientRect();
    if (!r) return;
    setPlace({ left: r.right > window.innerWidth - MARGIN, shiftUp: Math.max(0, r.bottom - (window.innerHeight - MARGIN)) });
  }, []);
  return (
    <div ref={ref} style={{ top: -4 - place.shiftUp }} className={cn("absolute z-10", place.left ? "right-full pr-0.5" : "left-full pl-0.5")}>
      {children}
    </div>
  );
}
