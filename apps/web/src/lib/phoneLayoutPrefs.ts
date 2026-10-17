export type PhoneLayout = "list" | "grid" | "focus";

export const PHONE_LAYOUTS: readonly PhoneLayout[] = ["list", "grid", "focus"];
export const DEFAULT_PHONE_LAYOUT: PhoneLayout = "grid";

const STORAGE_PREFIX = "openvms.livephone.layout.v1";

/** One saved layout per tenant and user, in the browser (same approach as the Vision AI view). */
export function phoneLayoutKey(tenantId: string | null, userId: string): string {
  return `${STORAGE_PREFIX}:${tenantId ?? "platform"}:${userId}`;
}

export function parsePhoneLayout(raw: string | null): PhoneLayout {
  if (!raw) return DEFAULT_PHONE_LAYOUT;
  try {
    const parsed = JSON.parse(raw) as { layout?: unknown } | null;
    return PHONE_LAYOUTS.find((layout) => layout === parsed?.layout) ?? DEFAULT_PHONE_LAYOUT;
  } catch {
    return DEFAULT_PHONE_LAYOUT;
  }
}

export function loadPhoneLayout(key: string): PhoneLayout {
  try {
    return parsePhoneLayout(localStorage.getItem(key));
  } catch {
    return DEFAULT_PHONE_LAYOUT;
  }
}

export function savePhoneLayout(key: string, layout: PhoneLayout): void {
  try {
    localStorage.setItem(key, JSON.stringify({ layout }));
  } catch {
    // Storage may be unavailable or full; the layout simply is not remembered.
  }
}
