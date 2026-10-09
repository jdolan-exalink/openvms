import { useSyncExternalStore } from "react";

/** The non-standard event Chromium fires when the app passes installability checks. */
type BeforeInstallPromptEvent = Event & {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
};

// The event can fire before React mounts, so the store listens from module load.
let deferred: BeforeInstallPromptEvent | null = null;
const listeners = new Set<() => void>();
const notify = () => listeners.forEach((listener) => listener());

if (typeof window !== "undefined") {
  window.addEventListener("beforeinstallprompt", (event) => {
    event.preventDefault();
    deferred = event as BeforeInstallPromptEvent;
    notify();
  });
  window.addEventListener("appinstalled", () => {
    deferred = null;
    notify();
  });
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};
const getCanInstall = () => deferred !== null;

function detectStandalone(): boolean {
  const iosStandalone = (navigator as Navigator & { standalone?: boolean }).standalone === true;
  return iosStandalone || (typeof matchMedia === "function" && matchMedia("(display-mode: standalone)").matches);
}

function detectIOS(): boolean {
  const ua = navigator.userAgent ?? "";
  if (/iPhone|iPad|iPod/.test(ua)) return true;
  // iPadOS Safari reports a desktop Mac user agent; touch support tells them apart.
  return navigator.platform === "MacIntel" && (navigator.maxTouchPoints ?? 0) > 1;
}

export type InstallPrompt = {
  /** True while the browser holds an install prompt we can trigger. */
  canInstall: boolean;
  promptInstall: () => Promise<"accepted" | "dismissed" | "unavailable">;
  isStandalone: boolean;
  isIOS: boolean;
};

/** useInstallPrompt exposes the browser install flow; iOS has no event, so callers show a hint. */
export function useInstallPrompt(): InstallPrompt {
  const canInstall = useSyncExternalStore(subscribe, getCanInstall, () => false);

  const promptInstall = async () => {
    const event = deferred;
    if (!event) return "unavailable" as const;
    await event.prompt();
    const { outcome } = await event.userChoice;
    // A prompt event is single use whatever the answer.
    deferred = null;
    notify();
    return outcome;
  };

  return { canInstall, promptInstall, isStandalone: detectStandalone(), isIOS: detectIOS() };
}
