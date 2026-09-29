import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { Check } from "lucide-react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cn } from "@/lib/cn";
import { Button } from "./ui";

export const severityLabel: Record<Schemas["NotificationSeverity"], string> = {
  info: "Info",
  warning: "Advertencia",
  critical: "Crítica",
};

const severityDot: Record<Schemas["NotificationSeverity"], string> = {
  info: "bg-accent",
  warning: "bg-warn",
  critical: "bg-bad",
};

/** Mutations shared by the bell panel and the Notificaciones screen. */
export function useNotificationActions() {
  const qc = useQueryClient();
  const refresh = () => void qc.invalidateQueries({ queryKey: ["notifications"] });
  const markRead = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/api/v1/notifications/{id}/read", { params: { path: { id } } })),
    onSuccess: refresh,
  });
  const markAll = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/notifications/read-all")),
    onSuccess: refresh,
  });
  return { markRead, markAll };
}

export function NotificationRow({
  item,
  onMarkRead,
  onOpen,
}: {
  item: Schemas["Notification"];
  onMarkRead: (id: string) => void;
  onOpen?: () => void;
}) {
  const router = useRouter();
  const unread = !item.read_at;
  const open = () => {
    if (unread) onMarkRead(item.id);
    if (item.link) {
      router.history.push(item.link);
      onOpen?.();
    }
  };
  const content = (
    <>
      <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", severityDot[item.severity])} aria-hidden />
      <span className="flex min-w-0 flex-col gap-0.5 text-left">
        <span className={cn("text-sm", unread ? "font-semibold" : "text-muted")}>{item.title}</span>
        {item.body && <span className="text-xs text-muted">{item.body}</span>}
        <time dateTime={item.created_at} className="font-mono text-[10px] text-muted">
          {new Date(item.created_at).toLocaleString("es")}
        </time>
      </span>
    </>
  );
  return (
    <li className="flex items-start gap-2 border-t border-line px-3 py-2 first:border-t-0">
      {item.link ? (
        <button type="button" onClick={open} className="flex min-w-0 flex-1 items-start gap-2 hover:underline focus-visible:outline-2 focus-visible:outline-accent">
          {content}
        </button>
      ) : (
        <div className="flex min-w-0 flex-1 items-start gap-2">{content}</div>
      )}
      {unread && (
        <Button aria-label={`Marcar como leída: ${item.title}`} title="Marcar como leída" onClick={() => onMarkRead(item.id)}>
          <Check className="size-3.5" aria-hidden />
        </Button>
      )}
    </li>
  );
}
