import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Schemas } from "@/api/client";
import { serverFrigateRawQuery } from "@/api/queries";
import { summarizeRecording } from "@/components/servers/storageRetention";
import { Button } from "@/components/ui";
import { useT } from "@/i18n";

function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex flex-col gap-3 rounded-m3-xl bg-surface-2 p-4">
      <h3 className="text-sm font-bold">{title}</h3>
      {children}
    </section>
  );
}

/**
 * ServerFrigateTab shows the Frigate configuration entry point and the recording settings of the
 * server. The raw config can contain secrets, so it is only requested when canReadConfig is set.
 */
export function ServerFrigateTab({
  server,
  canReadConfig,
  onOpenEditor,
}: {
  server: Schemas["Server"];
  canReadConfig: boolean;
  onOpenEditor: () => void;
}) {
  const t = useT();
  const raw = useQuery({ ...serverFrigateRawQuery(server.id), enabled: canReadConfig });
  const summary = raw.data ? summarizeRecording(raw.data.yaml) : null;
  const days = (value?: string) => (value ? t("servers.recordingDays", { count: value }) : t("servers.recordingNotSet"));

  return (
    <div className="flex flex-col gap-4">
      <Card title={t("servers.frigateConfigCard")}>
        <p className="text-sm text-muted">{t("servers.frigateConfigHint")}</p>
        <div>
          <Button size="sm" variant="tonal" onClick={onOpenEditor}>
            {t("servers.frigateOpenEditor")}
          </Button>
        </div>
      </Card>
      <Card title={t("servers.recordingCard")}>
        {!canReadConfig ? (
          <p role="note" className="text-xs text-muted">{t("servers.recordingNeedsPermission")}</p>
        ) : raw.isPending ? (
          <p className="text-xs text-muted">{t("servers.recordingLoading")}</p>
        ) : raw.isError ? (
          <p role="alert" className="text-xs text-bad">{t("servers.recordingError")}</p>
        ) : (
          <dl className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
            <div>
              <dt className="text-xs text-muted">{t("servers.recordingRetainDays")}</dt>
              <dd>{days(summary?.retainDays)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted">{t("servers.recordingRetainMode")}</dt>
              <dd>{summary?.retainMode ?? t("servers.recordingNotSet")}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted">{t("servers.snapshotsRetainDays")}</dt>
              <dd>{days(summary?.snapshotsRetainDays)}</dd>
            </div>
          </dl>
        )}
      </Card>
    </div>
  );
}
