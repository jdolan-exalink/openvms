import { afterEach, describe, expect, it } from "vitest";
import { getLocale, LOCALES, messageCatalogs, setLocale, sourceModules, translate, type MessageKey } from "./index";

afterEach(() => setLocale("es"));

function sourceKeys(): string[] {
  const keys: string[] = [];
  for (const [name, messages] of Object.entries(sourceModules)) {
    for (const key of Object.keys(messages)) keys.push(`${name}.${key}`);
  }
  return keys;
}

describe("interface language", () => {
  it("keeps Spanish as the source and translates the other shipped languages", () => {
    expect(translate("es", "nav.live")).toBe("En vivo");
    expect(translate("en", "nav.live")).toBe("Live");
    expect(translate("pt", "nav.cameras")).toBe("Câmeras");
    expect(translate("en", "dashboard.onlineCount", { count: 3 })).toBe("3 online");
  });

  it("falls back to Spanish and then to the key itself", () => {
    expect(translate("zz", "nav.live")).toBe("En vivo");
    expect(translate("en", "nav.missing" as MessageKey)).toBe("nav.missing");
  });

  it("discovers languages from their folders and keeps the same keys", () => {
    expect(LOCALES.map((item) => item.id)).toEqual(["es", "en", "pt"]);
    expect(LOCALES.find((item) => item.id === "pt")?.html).toBe("pt-BR");
    const expected = sourceKeys();
    for (const locale of LOCALES) {
      const pack = messageCatalogs[locale.id] ?? {};
      expect(Object.keys(pack).sort(), locale.id).toEqual([...expected].sort());
    }
  });

  it("remembers the choice for the next visit", () => {
    setLocale("pt");
    expect(getLocale()).toBe("pt");
    expect(localStorage.getItem("openvms.locale")).toBe("pt");
    expect(document.documentElement.lang).toBe("pt-BR");
    setLocale("zz");
    expect(getLocale()).toBe("pt");
  });
});
