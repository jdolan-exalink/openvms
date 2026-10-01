/**
 * Synthetic fixture generator for the maps performance harness (M-W10). It emits camera
 * entities in the compact wire format `GET /maps/sites/{id}/entities` answers with, so a
 * perf smoke can serve thousands of them without a backend. The module stays import-free
 * on purpose: the Playwright package loads it directly, outside the app's `@/` aliases.
 */

export interface PerfFixtureOptions {
  /** Site id every entity belongs to. */
  siteId?: string;
  /** Centre of the spread; defaults to the map's null island so any default zoom fits. */
  center?: { lat: number; lng: number };
  /** Half-side of the lat/lng box the cameras spread across, in degrees. */
  spreadDeg?: number;
  /** Seed for the deterministic PRNG: the same seed rebuilds the exact same site. */
  seed?: number;
}

export interface PerfEntity {
  id: string;
  t: "camera";
  site: string;
  srv: string;
  name: string;
  pos: { k: "geo"; lat: number; lng: number };
  st: string;
  rev: number;
  cam: { bearing: number; fov: number; range: number; type: string; ptz: boolean; lpr: boolean };
}

export const PERF_SITE_ID = "perf-site";
export const PERF_FIXTURE_REVISION = "perf-1";

const DEFAULT_SEED = 20261001;

/** mulberry32: tiny, fast and fully deterministic for a 32-bit seed. */
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/**
 * generatePerfEntities builds `count` cameras spread across a small box around `center`
 * so every marker fits the default viewport. Statuses follow a realistic mix (most
 * online, a few offline and degraded) instead of one uniform colour.
 */
export function generatePerfEntities(count: number, opts: PerfFixtureOptions = {}): PerfEntity[] {
  const siteId = opts.siteId ?? PERF_SITE_ID;
  const center = opts.center ?? { lat: 0, lng: 0 };
  const spread = opts.spreadDeg ?? 0.05;
  const random = mulberry32(opts.seed ?? DEFAULT_SEED);

  return Array.from({ length: count }, (_, i) => {
    const roll = random();
    const st = roll < 0.9 ? "online" : roll < 0.97 ? "offline" : "degraded";
    return {
      id: `perf-${String(i).padStart(6, "0")}`,
      t: "camera",
      site: siteId,
      srv: "srv-perf",
      name: `Perf cam ${i}`,
      pos: {
        k: "geo",
        lat: center.lat + (random() * 2 - 1) * spread,
        lng: center.lng + (random() * 2 - 1) * spread,
      },
      st,
      rev: 1,
      cam: {
        bearing: Math.round(random() * 360),
        fov: 70,
        range: 30,
        type: "fixed",
        ptz: false,
        lpr: false,
      },
    };
  });
}
