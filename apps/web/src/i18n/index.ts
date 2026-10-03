import { useSyncExternalStore } from "react";
import { catalog } from "./catalog";
import { isLocale, type Locale } from "./locale";

export { LOCALES, isLocale, type Locale } from "./locale";

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
  if (lang.startsWith("pt")) return "pt";
  if (lang.startsWith("en")) return "en";
  if (lang.startsWith("es")) return "es";
  return null;
}

function readInitial(): Locale {
  return readStored() ?? fromNavigator() ?? "es";
}

function apply(locale: Locale) {
  if (typeof document === "undefined") return;
  const html = locale === "pt" ? "pt-BR" : locale;
  document.documentElement.lang = html;
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

export function translate(locale: Locale, source: string, vars?: Vars): string {
  const table = locale === "es" ? undefined : catalog[locale];
  let text = table?.[source] ?? source;
  if (vars) {
    for (const [key, value] of Object.entries(vars)) text = text.replaceAll(`{${key}}`, String(value));
  }
  return text;
}

/** useT returns the translator for the active language and redraws when it changes. */
export function useT() {
  const locale = useSyncExternalStore(subscribe, getLocale, () => "es" as Locale);
  return (source: string, vars?: Vars) => translate(locale, source, vars);
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribe, getLocale, () => "es");
}
