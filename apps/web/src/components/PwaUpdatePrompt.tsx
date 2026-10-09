import { useRegisterSW } from "virtual:pwa-register/react";
import { useT } from "@/i18n";
import { Button } from "./ui";

/**
 * PwaUpdatePrompt registers the service worker and, when a new build is waiting, offers a snackbar.
 * Updating reloads the page, so it only happens when the operator asks (registerType "prompt").
 */
export function PwaUpdatePrompt() {
  const t = useT();
  const {
    needRefresh: [needRefresh, setNeedRefresh],
    updateServiceWorker,
  } = useRegisterSW();
  if (!needRefresh) return null;
  return (
    <div
      role="status"
      className="fixed inset-x-4 bottom-[calc(5rem+env(safe-area-inset-bottom))] z-[95] mx-auto flex max-w-md items-center gap-2 rounded-m3-md border border-outline-variant bg-surface-2 py-2 pl-4 pr-2 text-sm text-on-surface shadow-lg md:bottom-6 md:left-auto md:right-6 md:mx-0"
    >
      <span className="flex-1">{t("common.updateAvailable")}</span>
      <Button variant="text" size="sm" onClick={() => void updateServiceWorker(true)}>
        {t("common.updateAction")}
      </Button>
      <Button variant="text" size="sm" className="text-on-surface-variant" onClick={() => setNeedRefresh(false)}>
        {t("common.updateDismiss")}
      </Button>
    </div>
  );
}
