import { afterEach, describe, expect, it } from "vitest";
import { getLocale, setLocale, translate } from "./index";

afterEach(() => setLocale("es"));

describe("interface language", () => {
  it("keeps the Spanish source and translates English and Portuguese", () => {
    expect(translate("es", "En vivo")).toBe("En vivo");
    expect(translate("en", "En vivo")).toBe("Live");
    expect(translate("pt", "Cámaras")).toBe("Câmeras");
    expect(translate("en", "{count} en línea", { count: 3 })).toBe("3 online");
  });

  it("falls back to Spanish when a string has no translation yet", () => {
    expect(translate("en", "Texto todavía sin catálogo")).toBe("Texto todavía sin catálogo");
  });

  it("remembers the choice for the next visit", () => {
    setLocale("pt");
    expect(getLocale()).toBe("pt");
    expect(localStorage.getItem("openvms.locale")).toBe("pt");
    expect(document.documentElement.lang).toBe("pt-BR");
  });
});
