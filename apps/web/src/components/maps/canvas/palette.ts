import type { ExpressionSpecification } from "maplibre-gl";

/**
 * Canvas palette for the map layers. MapLibre paints with concrete color strings, so the
 * values are read from the active theme's CSS role variables (--md-*) each time a layer
 * spec is built; layers are rebuilt on every style reload, which the MapStyleController
 * triggers on a data-theme change. The hex literals below are the only documented
 * fallbacks (Ristretto values) for environments without computed styles (SSR, tests).
 */
export interface MapPalette {
  /** Selected marker / accent. */
  primary: string;
  /** Healthy / online. */
  ok: string;
  /** Degraded / warnings. */
  warn: string;
  /** Alarms / critical. */
  bad: string;
  /** Offline / unknown. */
  muted: string;
  /** Ink drawn on top of the status colors (icons, counts). */
  onMarker: string;
  /** Ring that separates a marker from the base map. */
  ring: string;
  /** Map labels. */
  label: string;
  /** Halo that keeps labels readable over the base map. */
  labelHalo: string;
}

const FALLBACK: MapPalette = {
  primary: "#f38d70",
  ok: "#adda78",
  warn: "#f9cc6c",
  bad: "#fd6883",
  muted: "#a39798",
  onMarker: "#2e140b",
  ring: "#fff1f3",
  label: "#c9bdbe",
  labelHalo: "#1f1a1a",
};

const VARIABLES: Record<keyof MapPalette, string> = {
  primary: "--md-primary",
  ok: "--md-success",
  warn: "--md-warning",
  bad: "--md-danger",
  muted: "--md-muted",
  onMarker: "--md-on-primary",
  ring: "--md-on-surface",
  label: "--md-on-surface-variant",
  labelHalo: "--md-surface-dim",
};

export function readMapPalette(): MapPalette {
  if (typeof window === "undefined" || typeof document === "undefined") return { ...FALLBACK };
  const computed = getComputedStyle(document.documentElement);
  const palette = { ...FALLBACK };
  for (const key of Object.keys(VARIABLES) as Array<keyof MapPalette>) {
    palette[key] = computed.getPropertyValue(VARIABLES[key]).trim() || FALLBACK[key];
  }
  return palette;
}

/** Connectivity token (`st` feature property) to status color. */
export function connectivityColorExpression(palette: MapPalette) {
  return [
    "match",
    ["get", "st"],
    "online",
    palette.ok,
    ["degraded", "recording_error"],
    palette.warn,
    palette.muted,
  ] as unknown as ExpressionSpecification;
}
