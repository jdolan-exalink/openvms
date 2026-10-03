import { useT } from "@/i18n";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { notificationsQuery } from "@/api/queries";
import { NotificationRow, useNotificationActions } from "@/components/notificationParts";
import { Button, Empty, ErrorNote, PageHeader, Summary } from "@/components/ui";

/** Notificaciones: full in-app inbox fed by rules; realtime pushes invalidate this list. */
export function Notifications() {
  const t = useT();
  const [unreadOnly, setUnreadOnly] = useState(false);
  const list = useQuery(notificationsQuery({ unread_only: unreadOnly || undefined, limit: 100 }));
  const { markRead, markAll } = useNotificationActions();
  const unread = list.data?.unread_count ?? 0;

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <PageHeader
        title={t("nav.notifications")}
        description={t("settings.notifications")}
        actions={
          <Button disabled={unread === 0 || markAll.isPending} onClick={() => markAll.mutate()}>
            Marcar todas como leídas
          </Button>
        }
      />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={unreadOnly} onChange={(e) => setUnreadOnly(e.target.checked)} className="rounded border-line" />
          Solo sin leer
        </label>
        {list.data && <Summary>{unread === 1 ? "1 sin leer" : `${unread} sin leer`}</Summary>}
      </div>
      <ErrorNote error={list.error ?? markRead.error ?? markAll.error} />
      {list.data?.items.length === 0 && <Empty>No hay notificaciones.</Empty>}
      {!!list.data?.items.length && (
        <ul aria-label="Notificaciones" className="rounded border border-line bg-surface">
          {list.data.items.map((n) => (
            <NotificationRow key={n.id} item={n} onMarkRead={(id) => markRead.mutate(id)} />
          ))}
        </ul>
      )}
    </div>
  );
}
