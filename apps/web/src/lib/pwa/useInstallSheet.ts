import { useCallback, useState } from "react";
import { useInstallPrompt } from "./useInstallPrompt";

export const INSTALL_DISMISSED_KEY = "openvms.installPrompt.dismissed";

function wasDismissed(): boolean {
  try {
    return localStorage.getItem(INSTALL_DISMISSED_KEY) === "1";
  } catch {
    return false;
  }
}

/** rememberInstallDismissal stops the sheet from opening by itself again on this device. */
export function rememberInstallDismissal() {
  try {
    localStorage.setItem(INSTALL_DISMISSED_KEY, "1");
  } catch {
    // Without storage the sheet may reappear next visit; harmless.
  }
}

const isSmallScreen = () => typeof matchMedia === "function" && matchMedia("(max-width: 767px)").matches;

/**
 * useInstallSheet owns the sheet's open state: it opens by itself at most once per device, on small
 * screens, when the app is installable (or iOS Safari, where installing is manual), and `show`
 * reopens it from the account menu at any time.
 */
export function useInstallSheet() {
  const { canInstall, isIOS, isStandalone } = useInstallPrompt();
  const [dismissed, setDismissed] = useState(wasDismissed);
  const [manual, setManual] = useState(false);
  const eligible = !isStandalone && (canInstall || isIOS);
  // Derived, not synced in an effect: installability can arrive after mount (beforeinstallprompt).
  const open = manual || (eligible && isSmallScreen() && !dismissed);

  const show = useCallback(() => setManual(true), []);
  const dismiss = useCallback(() => {
    rememberInstallDismissal();
    setDismissed(true);
    setManual(false);
  }, []);
  return { open, show, dismiss };
}
