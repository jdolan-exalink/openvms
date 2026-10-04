import { useCallback, useSyncExternalStore } from "react";

export type ThemeId = "ristretto" | "dracula" | "light";

export const THEME_STORAGE_KEY = "openvms.theme";
export const THEME_CHANGE_EVENT = "openvms:themechange";
export const THEME_IDS: readonly ThemeId[] = ["ristretto", "dracula", "light"];

/** Surface color per theme; mirrors --md-surface and feeds <meta name="theme-color">. */
const THEME_SURFACE: Record<ThemeId, string> = {
  ristretto: "#2c2525",
  dracula: "#282a36",
  light: "#f7f7fa",
};

export function isThemeId(value: unknown): value is ThemeId {
  return typeof value === "string" && (THEME_IDS as readonly string[]).includes(value);
}

export function isDarkTheme(id: ThemeId): boolean {
  return id !== "light";
}

function readStored(): string | null {
  try {
    return localStorage.getItem(THEME_STORAGE_KEY);
  } catch {
    return null;
  }
}

function systemPrefersLight(): boolean {
  try {
    return typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: light)").matches;
  } catch {
    return false;
  }
}

/**
 * Stored valid id wins; the legacy values ("light" is already an id, "dark" or anything else)
 * migrate to ristretto; with nothing stored the system preference decides.
 */
export function resolveInitialTheme(): ThemeId {
  const stored = readStored();
  if (isThemeId(stored)) return stored;
  if (stored) return "ristretto";
  return systemPrefersLight() ? "light" : "ristretto";
}

/** Reads the theme currently applied to the document, falling back to ristretto. */
export function currentTheme(): ThemeId {
  if (typeof document === "undefined") return "ristretto";
  const value = document.documentElement.dataset.theme;
  return isThemeId(value) ? value : "ristretto";
}

function syncThemeColorMeta(color: string) {
  let meta = document.head.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  if (!meta) {
    meta = document.createElement("meta");
    meta.name = "theme-color";
    document.head.appendChild(meta);
  }
  meta.content = color;
}

export function applyTheme(id: ThemeId): void {
  const root = document.documentElement;
  root.dataset.theme = id;
  root.style.colorScheme = isDarkTheme(id) ? "dark" : "light";
  syncThemeColorMeta(THEME_SURFACE[id]);
  try {
    localStorage.setItem(THEME_STORAGE_KEY, id);
  } catch {
    // The theme still applies for this session.
  }
  window.dispatchEvent(new CustomEvent(THEME_CHANGE_EVENT, { detail: { theme: id } }));
}

function subscribe(onChange: () => void) {
  window.addEventListener(THEME_CHANGE_EVENT, onChange);
  return () => window.removeEventListener(THEME_CHANGE_EVENT, onChange);
}

/** useTheme returns the active theme and a setter that applies and persists it. */
export function useTheme(): [ThemeId, (id: ThemeId) => void] {
  const theme = useSyncExternalStore(subscribe, currentTheme, () => "ristretto" as ThemeId);
  const setTheme = useCallback((id: ThemeId) => applyTheme(id), []);
  return [theme, setTheme];
}
