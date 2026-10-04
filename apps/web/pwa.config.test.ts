import { describe, expect, it } from "vitest";
import { pwaOptions } from "./pwa.config";

const workbox = pwaOptions.workbox!;
const denied = (path: string) => (workbox.navigateFallbackDenylist ?? []).some((re) => re.test(path));

describe("pwa options", () => {
  it("asks before updating and registers manually", () => {
    expect(pwaOptions.registerType).toBe("prompt");
    expect(pwaOptions.injectRegister).toBeNull();
  });

  it("describes an installable standalone app", () => {
    const manifest = pwaOptions.manifest as Exclude<typeof pwaOptions.manifest, false | undefined>;
    expect(manifest).toMatchObject({
      name: "OpenVMS",
      short_name: "OpenVMS",
      lang: "es",
      start_url: "/",
      scope: "/",
      display: "standalone",
      orientation: "any",
      background_color: "#2c2525",
      theme_color: "#2c2525",
    });
    const icons = manifest.icons ?? [];
    const find = (sizes: string, purpose?: string) => icons.find((i) => i.sizes === sizes && i.purpose === purpose);
    expect(find("192x192", "any")?.src).toBe("pwa-192x192.png");
    expect(find("512x512", "any")?.src).toBe("pwa-512x512.png");
    expect(find("512x512", "maskable")?.src).toBe("maskable-512x512.png");
    expect(icons.some((i) => i.type === "image/svg+xml" && i.sizes === "any")).toBe(true);
  });

  it("never serves API, media or realtime paths as the SPA shell", () => {
    for (const path of ["/api/x", "/api/v1/me", "/media/x", "/ws", "/ws/feed", "/health/x", "/openapi.json", "/docs"]) {
      expect(denied(path), path).toBe(true);
    }
    for (const path of ["/live", "/maps", "/events", "/account"]) expect(denied(path), path).toBe(false);
    expect(workbox.navigateFallback).toBe("/index.html");
  });

  it("has no runtime caching, so API and media always hit the network", () => {
    const rules = workbox.runtimeCaching ?? [];
    for (const rule of rules) {
      const matcher = rule.urlPattern;
      for (const path of ["/api/v1/me", "/media/a.mp4", "/ws"]) {
        const url = new URL(path, "https://vms.test");
        const hit = matcher instanceof RegExp ? matcher.test(url.href) : typeof matcher === "string" ? url.pathname === matcher : true;
        expect(hit, path).toBe(false);
      }
    }
    expect(workbox.cleanupOutdatedCaches).toBe(true);
  });

  it("precaches only build assets and keeps dev SW off", () => {
    expect(workbox.globPatterns).toEqual(["**/*.{js,css,html,svg,png,woff2}"]);
    expect(pwaOptions.devOptions?.enabled).toBeFalsy();
  });
});
