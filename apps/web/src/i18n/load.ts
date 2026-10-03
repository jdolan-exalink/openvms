import type { MessageKey } from "./modules";

export type LocaleMeta = { id: string; label: string; short: string; html: string; order?: number };

type MessageModule = { default?: Record<string, string> };
type MetaModule = { localeMeta?: { label: string; short: string; html: string; order?: number } };

const messageFiles = import.meta.glob("./locales/*/*.ts", { eager: true }) as Record<string, MessageModule>;
const metaFiles = import.meta.glob("./locales/*/meta.ts", { eager: true }) as Record<string, MetaModule>;

/** Languages without `order` in meta.ts sort after the numbered ones, then by id. */
const defaultOrder = 1000;

function localeOf(path: string): { locale: string; name: string } | null {
  const match = /\/locales\/([^/]+)\/([^/]+)\.ts$/.exec(path);
  if (!match) return null;
  const locale = match[1] ?? "";
  const name = (match[2] ?? "").replace(/\.ts$/, "");
  if (!locale || locale.startsWith("_") || !name || name === "meta") return null;
  return { locale, name };
}

function buildCatalogs(): Record<string, Record<string, string>> {
  const catalogs: Record<string, Record<string, string>> = {};
  for (const [path, mod] of Object.entries(messageFiles)) {
    const parsed = localeOf(path);
    const messages = mod.default;
    if (!parsed || !messages) continue;
    const pack = (catalogs[parsed.locale] ??= {});
    for (const [key, value] of Object.entries(messages)) pack[`${parsed.name}.${key}`] = value;
  }
  return catalogs;
}

function buildMetas(ids: string[]): LocaleMeta[] {
  const byId = new Map<string, LocaleMeta>();
  for (const [path, mod] of Object.entries(metaFiles)) {
    const match = /\/locales\/([^/]+)\/meta\.ts$/.exec(path);
    const id = match?.[1];
    if (!id || id.startsWith("_") || !ids.includes(id)) continue;
    const meta = mod.localeMeta;
    byId.set(id, {
      id,
      label: meta?.label || id,
      short: meta?.short || id.toUpperCase(),
      html: meta?.html || id,
      order: meta?.order,
    });
  }
  for (const id of ids) {
    if (!byId.has(id)) byId.set(id, { id, label: id, short: id.toUpperCase(), html: id });
  }
  return [...byId.values()].sort((a, b) => {
    const ao = a.order ?? defaultOrder;
    const bo = b.order ?? defaultOrder;
    if (ao !== bo) return ao - bo;
    return a.id.localeCompare(b.id);
  });
}

export const messageCatalogs = buildCatalogs();
export const LOCALES: readonly LocaleMeta[] = buildMetas(Object.keys(messageCatalogs));
export const fallbackLocale = messageCatalogs.es ? "es" : (LOCALES[0]?.id ?? "es");

export type Locale = string;

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && value in messageCatalogs;
}

type Vars = Record<string, string | number>;

export function translate(locale: string, key: MessageKey, vars?: Vars): string {
  const pack = messageCatalogs[locale] ?? messageCatalogs[fallbackLocale];
  let text = pack?.[key] ?? messageCatalogs[fallbackLocale]?.[key] ?? key;
  if (vars) {
    for (const [name, value] of Object.entries(vars)) text = text.replaceAll(`{${name}}`, String(value));
  }
  return text;
}
