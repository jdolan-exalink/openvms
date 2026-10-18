import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { meQuery } from "@/api/queries";
import { ConfigTab } from "@/components/faces/ConfigTab";
import { PersonsTab } from "@/components/faces/PersonsTab";
import { RecognitionsTab } from "@/components/faces/RecognitionsTab";
import { TrainTab } from "@/components/faces/TrainTab";
import { WatchlistsTab } from "@/components/faces/WatchlistsTab";
import { Empty, PageHeader } from "@/components/ui";
import { useT, type MessageKey } from "@/i18n";
import { cn } from "@/lib/cn";
import { can } from "@/lib/perm";

type Tab = "people" | "train" | "recognitions" | "lists" | "config";
const tabs: { id: Tab; label: MessageKey }[] = [
  { id: "people", label: "faces.tabPeople" },
  { id: "train", label: "faces.tabTrain" },
  { id: "recognitions", label: "faces.tabRecognitions" },
  { id: "lists", label: "faces.tabLists" },
  { id: "config", label: "faces.tabConfig" },
];

/**
 * Faces: the central person registry, training from Frigate's unclassified attempts, and the
 * recognition timeline. Everything needs faces.view; changes need faces.manage (the API
 * enforces both regardless of what is shown here).
 */
export function Faces() {
  const t = useT();
  const me = useQuery(meQuery);
  const [tab, setTab] = useState<Tab>("people");
  const canView = can(me.data, "faces.view");
  const canManage = can(me.data, "faces.manage");
  // The settings live on the servers, so they also need servers.view (the API checks it per server).
  const canConfig = canView && can(me.data, "servers.view");
  const visibleTabs = tabs.filter((item) => item.id !== "config" || canConfig);

  if (me.isSuccess && !canView) {
    return (
      <div className="mx-auto flex max-w-6xl flex-col gap-6">
        <PageHeader title={t("faces.title")} />
        <Empty>{t("faces.noAccess")}</Empty>
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title={t("faces.title")} description={t("faces.description")} />
      {canView && (
        <>
          <div role="tablist" aria-label={t("faces.tabs")} className="flex gap-1 border-b border-outline-variant">
            {visibleTabs.map((item) => (
              <button
                key={item.id}
                type="button"
                role="tab"
                id={`faces-tab-${item.id}`}
                aria-selected={tab === item.id}
                aria-controls={`faces-panel-${item.id}`}
                onClick={() => setTab(item.id)}
                className={cn(
                  "h-11 border-b-2 px-4 text-sm font-medium focus-visible:outline-2 focus-visible:outline-primary",
                  tab === item.id ? "border-primary text-primary" : "border-transparent text-on-surface-variant hover:bg-on-surface/8",
                )}
              >
                {t(item.label)}
              </button>
            ))}
          </div>
          <div role="tabpanel" id={`faces-panel-${tab}`} aria-labelledby={`faces-tab-${tab}`}>
            {tab === "people" && <PersonsTab canManage={canManage} />}
            {tab === "train" && <TrainTab canManage={canManage} />}
            {tab === "recognitions" && <RecognitionsTab />}
            {tab === "lists" && <WatchlistsTab canManage={canManage} />}
            {tab === "config" && canConfig && <ConfigTab canRestart={can(me.data, "servers.restart")} />}
          </div>
        </>
      )}
    </div>
  );
}
