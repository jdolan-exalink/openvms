import type { VitePWAOptions } from "vite-plugin-pwa";

/**
 * PWA options, kept apart from vite.config.ts so the caching contract is unit-tested.
 *
 * Policy: the service worker precaches build assets only (the app shell works offline and
 * installs). API, media and realtime traffic are never cached: there is no runtime caching, so
 * those requests always go to the network, and navigations to those paths are excluded from the
 * SPA fallback so a browser never receives index.html in place of an API or media response.
 */
const NETWORK_ONLY_PATHS = [/^\/api(\/|$)/, /^\/media(\/|$)/, /^\/ws(\/|$)/, /^\/health(\/|$)/, /^\/openapi\.json$/, /^\/docs(\/|$)/];

export const pwaOptions: Partial<VitePWAOptions> = {
  // "prompt": a waiting worker never reloads the app mid-shift; the operator confirms the update.
  registerType: "prompt",
  // Registered by <PwaUpdatePrompt> through virtual:pwa-register/react.
  injectRegister: null,
  includeAssets: ["favicon.svg", "apple-touch-icon-180x180.png"],
  manifest: {
    name: "OpenVMS",
    short_name: "OpenVMS",
    lang: "es",
    start_url: "/",
    scope: "/",
    display: "standalone",
    orientation: "any",
    background_color: "#2c2525",
    theme_color: "#2c2525",
    icons: [
      { src: "pwa-192x192.png", sizes: "192x192", type: "image/png", purpose: "any" },
      { src: "pwa-512x512.png", sizes: "512x512", type: "image/png", purpose: "any" },
      { src: "maskable-512x512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
      { src: "icon.svg", sizes: "any", type: "image/svg+xml", purpose: "any" },
    ],
  },
  workbox: {
    globPatterns: ["**/*.{js,css,html,svg,png,woff2}"],
    navigateFallback: "/index.html",
    navigateFallbackDenylist: NETWORK_ONLY_PATHS,
    cleanupOutdatedCaches: true,
    // Stated explicitly (it equals the default) so a chunk growing past 2 MiB fails loudly instead of
    // silently dropping out of the precache. The largest chunks today are the pdf worker (~1.2 MB) and
    // MapCanvas (~1.1 MB), so they still fit; the whole precache is ~4.3 MiB.
    maximumFileSizeToCacheInBytes: 2 * 1024 * 1024,
  },
  devOptions: { enabled: false },
};
