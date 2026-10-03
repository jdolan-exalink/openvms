export const LOCALES = [
  { id: "es", label: "Español", short: "ES", html: "es" },
  { id: "en", label: "English", short: "EN", html: "en" },
  { id: "pt", label: "Português", short: "PT", html: "pt-BR" },
] as const;

export type Locale = (typeof LOCALES)[number]["id"];

export function isLocale(value: unknown): value is Locale {
  return value === "es" || value === "en" || value === "pt";
}
