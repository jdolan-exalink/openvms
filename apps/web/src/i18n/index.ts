import { useSyncExternalStore } from "react";
import { fallbackLocale, isLocale, LOCALES, messageCatalogs, translate, type Locale } from "./load";
import type { MessageKey } from "./modules";

export { LOCALES, fallbackLocale, isLocale, messageCatalogs, translate, type Locale, type LocaleMeta } from "./load";
export { sourceModules, type MessageKey } from "./modules";

const STORAGE_KEY = "openvms.locale";

type Vars = Record<string, string | number>;

let current: Locale = readInitial();
const listeners = new Set<() => void>();

function readStored(): Locale | null {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return isLocale(stored) ? stored : null;
  } catch {
    return null;
  }
}

function fromNavigator(): Locale | null {
  if (typeof navigator === "undefined") return null;
  const lang = navigator.language.toLowerCase();
  const match = (prefix: string) => Object.keys(messageCatalogs).find((id) => id === prefix || id.startsWith(`${prefix}-`)) ?? null;
  if (lang.startsWith("pt")) return match("pt");
  if (lang.startsWith("en")) return match("en");
  if (lang.startsWith("es")) return match("es");
  return null;
}

function readInitial(): Locale {
  return readStored() ?? fromNavigator() ?? fallbackLocale;
}

function apply(locale: Locale) {
  if (typeof document === "undefined") return;
  document.documentElement.lang = LOCALES.find((item) => item.id === locale)?.html ?? locale;
}

/** bootLocale applies the saved language before the first paint. */
export function bootLocale() {
  current = readInitial();
  apply(current);
}

export function getLocale(): Locale {
  return current;
}

export function setLocale(locale: Locale) {
  if (!isLocale(locale)) return;
  current = locale;
  try {
    localStorage.setItem(STORAGE_KEY, locale);
  } catch {
    // The choice still applies for this session.
  }
  apply(locale);
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** useT returns the translator for the active language and redraws when it changes. */
export function useT() {
  const locale = useSyncExternalStore(subscribe, getLocale, () => fallbackLocale);
  return (key: MessageKey, vars?: Vars) => translate(locale, key, vars);
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribe, getLocale, () => fallbackLocale);
}
