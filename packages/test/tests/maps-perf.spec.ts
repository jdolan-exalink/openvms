import { expect, test, type Page } from "@playwright/test";
import {
  PERF_FIXTURE_REVISION,
  PERF_SITE_ID,
  generatePerfEntities,
} from "../../../apps/web/src/lib/maps/perfFixture";

declare global {
  interface Window {
    __openvmsMapMetrics?: {
      fps: number;
      entitiesVisible: number;
      eventsPerSec: number;
      wsLagMs: number | null;
      timeToFirstRenderMs: number | null;
      maxLongTaskMs: number;
    };
  }
}

const ENTITY_COUNT = 5000;
// Design target: FPS ≥ 50 at 5k (desktop). Weaker shared CI hardware can lower the bar
// explicitly with MAPS_PERF_MIN_FPS instead of silently weakening the default.
const MIN_FPS = Number(process.env.MAPS_PERF_MIN_FPS ?? 50);
const MAX_LONG_TASK_MS = 50;
const MAX_FIRST_RENDER_MS = 5000;
const WARM_UP_MS = 5000;

const json = (body: unknown) => ({ contentType: "application/json", body: JSON.stringify(body) });
// 1×1 transparent PNG: the raster pipeline gets real tiles without any network.
const BLANK_TILE = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64",
);

const grants = (names: string[]) =>
  names.map(permission => ({ permission, effect: "allow", scope_type: "platform" }));

/**
 * stubBackend answers every API call from fixtures, so the smoke measures the client
 * render path (fetch → wire mapping → GeoJSON → MapLibre) with no backend at all.
 */
async function stubBackend(page: Page) {
  await page.route("**/perf-tiles/**", route =>
    route.fulfill({ contentType: "image/png", body: BLANK_TILE }));
  await page.route("**/api/v1/**", route => {
    const { pathname } = new URL(route.request().url());
    switch (pathname) {
      case "/api/v1/me":
        return route.fulfill(json({
          id: "u-perf", username: "perf", tenant_id: "t-perf",
          grants: grants(["maps.view", "maps.edit", "maps.edit_device", "maps.create_zone", "live.view"]),
        }));
      case "/api/v1/features":
        return route.fulfill(json({
          persistent_players: false, video_surface_layer: false, adaptive_streaming: false,
          stream_prewarming: false, seamless_quality_switch: false, maps: true,
        }));
      case "/api/v1/me/map-prefs":
        return route.fulfill(json({}));
      case "/api/v1/maps/config":
        return route.fulfill(json({
          provider: {
            id: "perf", kind: "raster", tiles: ["/perf-tiles/{z}/{x}/{y}.png"],
            attribution: "perf smoke", max_zoom: 18, offline: true,
          },
          default_center: { lat: 0, lng: 0 },
          default_zoom: 14,
        }));
      case "/api/v1/maps/overview":
        return route.fulfill(json({
          items: [{
            id: PERF_SITE_ID, name: "Perf Site", lat: 0, lng: 0, camera_count: ENTITY_COUNT,
            online_cameras: ENTITY_COUNT, offline_cameras: 0, degraded_cameras: 0, alarm_count: 0,
          }],
        }));
      case `/api/v1/maps/sites/${PERF_SITE_ID}/entities`:
        return route.fulfill(json({ revision: PERF_FIXTURE_REVISION, entities: generatePerfEntities(ENTITY_COUNT) }));
      default:
        return route.fulfill(json({ items: [] }));
    }
  });
}

test("maps renders 5k synthetic cameras above the performance floor", async ({ page }) => {
  const pageErrors: string[] = [];
  page.on("pageerror", error => pageErrors.push(String(error)));
  // Console errors are how a half-loaded map announces itself (missing worker, bad
  // source); the perf numbers alone would stay green either way.
  const consoleErrors: string[] = [];
  page.on("console", message => {
    if (message.type() !== "error") return;
    // The realtime /ws feed belongs to the real stack; this render-only smoke has no
    // backend, so its reconnect noise says nothing about the map.
    if (message.text().startsWith("WebSocket connection to")) return;
    consoleErrors.push(message.text());
  });
  await stubBackend(page);

  await page.goto(`/maps?site=${PERF_SITE_ID}&perf=1`);

  // Without real GL the fps numbers would be fiction: fail here with a clear cause.
  const webgl2 = await page.evaluate(() => !!document.createElement("canvas").getContext("webgl2"));
  expect(webgl2, "headless WebGL2 (SwiftShader) unavailable").toBe(true);

  // The overlay reports first render once entities are on screen; then let the rolling
  // fps window fill (30 frames) and the cold-start long tasks drain.
  await page.waitForFunction(() => window.__openvmsMapMetrics?.timeToFirstRenderMs !== null, undefined, { timeout: 60_000 });
  await page.waitForTimeout(WARM_UP_MS);

  const snapshot = await page.evaluate(() => window.__openvmsMapMetrics!);
  expect(snapshot.entitiesVisible).toBe(ENTITY_COUNT);
  expect(snapshot.fps).toBeGreaterThanOrEqual(MIN_FPS);
  expect(snapshot.maxLongTaskMs).toBeLessThanOrEqual(MAX_LONG_TASK_MS);
  expect(snapshot.timeToFirstRenderMs).toBeLessThan(MAX_FIRST_RENDER_MS);
  expect(pageErrors).toEqual([]);
  expect(consoleErrors).toEqual([]);
});
