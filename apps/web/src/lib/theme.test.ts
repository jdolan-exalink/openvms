import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { applyTheme, isDarkTheme, resolveInitialTheme, THEME_STORAGE_KEY } from "./theme";

function mockPrefersLight(matches: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: query.includes("light") ? matches : false,
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })) as unknown as typeof window.matchMedia;
}

beforeEach(() => {
  localStorage.clear();
  document.head.querySelectorAll('meta[name="theme-color"]').forEach((m) => m.remove());
  delete document.documentElement.dataset.theme;
  document.documentElement.style.removeProperty("color-scheme");
  mockPrefersLight(false);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("resolveInitialTheme", () => {
  it("returns a stored valid theme id", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dracula");
    expect(resolveInitialTheme()).toBe("dracula");
  });

  it("keeps the legacy stored light value as light", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "light");
    expect(resolveInitialTheme()).toBe("light");
  });

  it("maps the legacy stored dark value to ristretto", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "dark");
    expect(resolveInitialTheme()).toBe("ristretto");
  });

  it("maps any other non-empty stored value to ristretto", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "solarized");
    expect(resolveInitialTheme()).toBe("ristretto");
  });

  it("follows prefers-color-scheme light when nothing is stored", () => {
    mockPrefersLight(true);
    expect(resolveInitialTheme()).toBe("light");
  });

  it("defaults to ristretto when nothing is stored and the system is dark", () => {
    expect(resolveInitialTheme()).toBe("ristretto");
  });
});

describe("applyTheme", () => {
  it("sets data-theme and color-scheme", () => {
    applyTheme("dracula");
    expect(document.documentElement.dataset.theme).toBe("dracula");
    expect(document.documentElement.style.colorScheme).toBe("dark");
    applyTheme("light");
    expect(document.documentElement.style.colorScheme).toBe("light");
  });

  it("creates the theme-color meta when missing and updates it afterwards", () => {
    applyTheme("ristretto");
    const meta = document.head.querySelectorAll('meta[name="theme-color"]');
    expect(meta).toHaveLength(1);
    expect(meta[0]?.getAttribute("content")).toBe("#2c2525");
    applyTheme("light");
    expect(document.head.querySelectorAll('meta[name="theme-color"]')).toHaveLength(1);
    expect(document.head.querySelector('meta[name="theme-color"]')?.getAttribute("content")).toBe("#f7f7fa");
  });

  it("persists the choice", () => {
    applyTheme("dracula");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dracula");
  });

  it("dispatches openvms:themechange with the theme", () => {
    const handler = vi.fn();
    window.addEventListener("openvms:themechange", handler);
    applyTheme("light");
    window.removeEventListener("openvms:themechange", handler);
    expect(handler).toHaveBeenCalledTimes(1);
    expect((handler.mock.calls[0]?.[0] as CustomEvent).detail).toEqual({ theme: "light" });
  });
});

describe("isDarkTheme", () => {
  it("treats ristretto and dracula as dark and light as light", () => {
    expect(isDarkTheme("ristretto")).toBe(true);
    expect(isDarkTheme("dracula")).toBe(true);
    expect(isDarkTheme("light")).toBe(false);
  });
});
