import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Bell } from "lucide-react";
import { type RefObject, useEffect, useRef, useState } from "react";
import { notificationsQuery } from "@/api/queries";
import { useFocusTrap } from "@/lib/useFocusTrap";
import { NotificationRow, useNotificationActions } from "./notificationParts";
import { Button, ErrorNote } from "./ui";

const PANEL_LIMIT = 8;

/** Top-bar bell: unread badge plus a dropdown with the latest notifications. */
export function NotificationBell() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const list = useQuery(notificationsQuery({ limit: PANEL_LIMIT }));
  const unread = list.data?.unread_count ?? 0;

  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        ref={buttonRef}
        type="button"
        aria-label={unread > 0 ? `Notificaciones, ${unread} sin leer` : "Notificaciones"}
        aria-expanded={open}
        aria-haspopup="true"
        onClick={() => setOpen((v) => !v)}
        className="relative flex size-9 items-center justify-center rounded-lg text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
      >
        <Bell className="size-4" aria-hidden />
        {unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-bad px-1 text-center text-[10px] font-semibold leading-4 text-white tabular-nums">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      {open && <NotificationPanel returnFocusTo={buttonRef} onClose={() => setOpen(false)} />}
    </div>
  );
}

function NotificationPanel({ returnFocusTo, onClose }: { returnFocusTo: RefObject<HTMLElement | null>; onClose: () => void }) {
  const panelRef = useRef<HTMLDivElement>(null);
  const list = useQuery(notificationsQuery({ limit: PANEL_LIMIT }));
  const { markRead, markAll } = useNotificationActions();
  const unread = list.data?.unread_count ?? 0;
  useFocusTrap(panelRef, onClose, returnFocusTo);

  return (
    <div
      ref={panelRef}
      role="region"
      aria-label="Panel de notificaciones"
      tabIndex={-1}
      className="absolute right-0 z-40 mt-2 flex w-80 max-w-[90vw] flex-col rounded border border-line bg-surface shadow-2xl focus:outline-none"
    >
      <div className="flex items-center justify-between gap-2 border-b border-line px-3 py-2">
        <span className="text-sm font-medium">Notificaciones</span>
        <Button disabled={unread === 0 || markAll.isPending} onClick={() => markAll.mutate()}>
          Marcar todas como leídas
        </Button>
      </div>
      <ErrorNote error={list.error ?? markAll.error ?? markRead.error} />
      {list.data?.items.length === 0 && <p className="px-3 py-6 text-center text-sm text-muted">No hay notificaciones.</p>}
      <ul className="max-h-80 overflow-y-auto">
        {list.data?.items.map((n) => (
          <NotificationRow key={n.id} item={n} onMarkRead={(id) => markRead.mutate(id)} onOpen={onClose} />
        ))}
      </ul>
      <Link to="/notifications" onClick={onClose} className="border-t border-line px-3 py-2 text-center text-xs text-accent hover:underline">
        Ver todas
      </Link>
    </div>
  );
}
