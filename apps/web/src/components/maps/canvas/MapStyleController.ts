import * as maplibregl from "maplibre-gl";
import type { StyleSpecification } from "maplibre-gl";
import * as pmtiles from "pmtiles";
import type { MapProviderConfig } from "@/lib/maps/types";
import { currentTheme, isDarkTheme, type ThemeId } from "@/lib/theme";

let protocolRegistered = false;

export function registerPMTilesProtocol() {
  if (protocolRegistered) return;
  try {
    const protocol = new pmtiles.Protocol();
    maplibregl.addProtocol("pmtiles", protocol.tile);
    protocolRegistered = true;
  } catch (err) {
    console.warn("Failed to register PMTiles protocol", err);
  }
}

export interface ThemeColors {
  isDark: boolean;
  background: string;
  water: string;
  roadsMinor: string;
  roadsMajor: string;
  buildings: string;
  labels: string;
  labelsHalo: string;
}

export function getThemeColors(): ThemeColors {
  const isDark = typeof document !== "undefined" ? isDarkTheme(currentTheme()) : true;

  if (typeof window === "undefined") {
    return isDark ? getDarkFallbackColors() : getLightFallbackColors();
  }

  const computed = getComputedStyle(document.documentElement);
  const getVar = (name: string, fallback: string) => computed.getPropertyValue(name).trim() || fallback;

  if (isDark) {
    const bgApp = getVar("--bg-app", "#0e1523");
    const bgNav = getVar("--bg-nav", "#101827");
    const bgPanel = getVar("--bg-panel", "#151f30");
    const bgPanelHover = getVar("--bg-panel-hover", "#222e42");
    const borderDefault = getVar("--border-default", "#29364a");
    const textMuted = getVar("--text-muted", "#7e8a9a");

    return {
      isDark: true,
      background: bgApp,
      water: bgNav, // darkened nav tone
      roadsMinor: bgPanelHover,
      roadsMajor: borderDefault,
      buildings: bgPanel,
      labels: textMuted,
      labelsHalo: bgApp,
    };
  } else {
    const bgApp = getVar("--bg-app", "#f4f6f5");
    const bgPanel = getVar("--bg-panel", "#ffffff");
    const bgPanelElevated = getVar("--bg-panel-elevated", "#eef2f0");
    const borderDefault = getVar("--border-default", "#d6dfdc");
    const textSecondary = getVar("--text-secondary", "#586864");

    return {
      isDark: false,
      background: bgApp,
      water: "#d8e6e2", // tinted accent / water tone
      roadsMinor: borderDefault,
      roadsMajor: bgPanelElevated,
      buildings: bgPanelElevated,
      labels: textSecondary,
      labelsHalo: bgPanel,
    };
  }
}

function getDarkFallbackColors(): ThemeColors {
  return {
    isDark: true,
    background: "#0e1523",
    water: "#0a101a",
    roadsMinor: "#222e42",
    roadsMajor: "#29364a",
    buildings: "#151f30",
    labels: "#7e8a9a",
    labelsHalo: "#0e1523",
  };
}

function getLightFallbackColors(): ThemeColors {
  return {
    isDark: false,
    background: "#f4f6f5",
    water: "#d8e6e2",
    roadsMinor: "#d6dfdc",
    roadsMajor: "#eef2f0",
    buildings: "#eef2f0",
    labels: "#586864",
    labelsHalo: "#ffffff",
  };
}

const hostedFonts = new Set(["Noto Sans Regular", "Noto Sans Bold", "Noto Sans Italic"]);

function repairImageRef(value: unknown): unknown {
  if (value === "circle-11") return "circle_11";
  if (Array.isArray(value)) return value.map(repairImageRef);
  return value;
}

/** OpenFreeMap's dark style still names sprites and fonts the tile server no longer serves. */
export function repairHostedStyle(style: StyleSpecification): StyleSpecification {
  const next = structuredClone(style);
  for (const layer of next.layers) {
    const layout = layer.layout as Record<string, unknown> | undefined;
    const fonts = layout?.["text-font"];
    if (Array.isArray(fonts) && fonts.some((font) => typeof font === "string" && !hostedFonts.has(font))) {
      layout!["text-font"] = ["Noto Sans Regular"];
    }
    if (layout && "icon-image" in layout) layout["icon-image"] = repairImageRef(layout["icon-image"]);
    const paint = (layer as { paint?: Record<string, unknown> }).paint;
    if (paint?.["fill-pattern"] === "wood-pattern") {
      delete paint["fill-pattern"];
      paint["fill-color"] ??= "#1c3a2a";
      paint["fill-opacity"] ??= 0.4;
    }
  }
  return next;
}

export async function loadRepairedStyle(url: string): Promise<StyleSpecification> {
  const response = await fetch(url, { cache: "reload" });
  if (!response.ok) throw new Error(`No se pudo cargar el estilo del mapa (${response.status}).`);
  return repairHostedStyle((await response.json()) as StyleSpecification);
}

export function buildMapStyle(provider: MapProviderConfig, theme: ThemeColors = getThemeColors()): StyleSpecification {
  registerPMTilesProtocol();

  if (provider.kind === "vector-style") {
    const url = theme.isDark ? provider.styleUrl?.dark : provider.styleUrl?.light;
    if (url) {
      // In MapLibre, setStyle accepts a URL or StyleSpecification. For uniform handling, we provide a placeholder style that points to the URL or sources
      // When a full style URL is configured, callers can pass the URL directly to map.setStyle(url)
    }
  }

  if (provider.kind === "raster") {
    const tiles = provider.tiles && provider.tiles.length > 0 ? provider.tiles : ["https://tile.openstreetmap.org/{z}/{x}/{y}.png"];

    const rasterPaint: Record<string, unknown> = theme.isDark
      ? {
          "raster-brightness-max": 0.35,
          "raster-saturation": -0.6,
          "raster-contrast": -0.1,
          "raster-opacity": 0.95,
        }
      : {
          "raster-opacity": 1.0,
        };

    return {
      version: 8,
      name: `OpenVMS Raster (${theme.isDark ? "Dark" : "Light"})`,
      sources: {
        "raster-tiles": {
          type: "raster",
          tiles,
          tileSize: 256,
          attribution: provider.attribution || "© OpenStreetMap contributors",
          maxzoom: provider.maxZoom || 19,
        },
      },
      layers: [
        {
          id: "background",
          type: "background",
          paint: {
            "background-color": theme.background,
          },
        },
        {
          id: "raster-layer",
          type: "raster",
          source: "raster-tiles",
          paint: rasterPaint,
        },
      ],
    } as StyleSpecification;
  }

  // Default: PMTiles / Protomaps vector source
  const pmtilesUrl = provider.tiles?.[0] || "/tiles/base.pmtiles";
  const sourceUrl = pmtilesUrl.startsWith("pmtiles://") ? pmtilesUrl : `pmtiles://${pmtilesUrl}`;

  return {
    version: 8,
    name: `OpenVMS PMTiles (${theme.isDark ? "Dark" : "Light"})`,
    sources: {
      protomaps: {
        type: "vector",
        url: sourceUrl,
        attribution: provider.attribution || "© OpenStreetMap contributors",
      },
    },
    layers: [
      {
        id: "background",
        type: "background",
        paint: {
          "background-color": theme.background,
        },
      },
      {
        id: "water",
        type: "fill",
        source: "protomaps",
        "source-layer": "water",
        paint: {
          "fill-color": theme.water,
        },
      },
      {
        id: "landuse",
        type: "fill",
        source: "protomaps",
        "source-layer": "landuse",
        paint: {
          "fill-color": theme.background,
          "fill-opacity": 0.5,
        },
      },
      {
        id: "roads-minor",
        type: "line",
        source: "protomaps",
        "source-layer": "roads",
        filter: ["!in", "highway", "motorway", "trunk", "primary"],
        paint: {
          "line-color": theme.roadsMinor,
          "line-width": 1,
        },
      },
      {
        id: "roads-major",
        type: "line",
        source: "protomaps",
        "source-layer": "roads",
        filter: ["in", "highway", "motorway", "trunk", "primary"],
        paint: {
          "line-color": theme.roadsMajor,
          "line-width": 2,
        },
      },
      {
        id: "buildings",
        type: "fill",
        source: "protomaps",
        "source-layer": "buildings",
        paint: {
          "fill-color": theme.buildings,
          "fill-opacity": theme.isDark ? 0.8 : 0.9,
        },
      },
      {
        id: "labels",
        type: "symbol",
        source: "protomaps",
        "source-layer": "places",
        layout: {
          "text-field": ["get", "name"],
          "text-size": 11,
          "text-max-width": 8,
        },
        paint: {
          "text-color": theme.labels,
          "text-halo-color": theme.labelsHalo,
          "text-halo-width": 1.5,
        },
      },
    ],
  } as StyleSpecification;
}

export class MapStyleController {
  private map: maplibregl.Map | null = null;
  private provider: MapProviderConfig;
  private observer: MutationObserver | null = null;
  private currentTheme: ThemeId;
  private onReapplyCustomLayers?: () => void;
  private styleLoadPending = false;

  constructor(provider: MapProviderConfig, onReapplyCustomLayers?: () => void) {
    this.provider = provider;
    this.onReapplyCustomLayers = onReapplyCustomLayers;
    this.currentTheme = currentTheme();
  }

  public setProvider(provider: MapProviderConfig) {
    this.provider = provider;
    this.applyCurrentStyle();
  }

  public attach(map: maplibregl.Map) {
    this.map = map;

    // Listen to style reload to re-apply any custom layers (cameras, FOV, zones)
    this.map.on("style.load", () => {
      this.onReapplyCustomLayers?.();
    });

    if (typeof window !== "undefined" && typeof MutationObserver !== "undefined") {
      this.observer = new MutationObserver(() => {
        // Any palette change restyles the map: two dark themes still differ in their CSS variables.
        const theme = currentTheme();
        if (theme !== this.currentTheme) {
          this.currentTheme = theme;
          this.applyCurrentStyle();
        }
      });

      this.observer.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ["data-theme"],
      });
    }
  }

  public applyCurrentStyle() {
    if (!this.map) return;
    if (!this.map.isStyleLoaded()) {
      // setStyle cannot diff against a style that is still streaming in: MapLibre rebuilds
      // it from scratch mid-load ("Unable to perform style diff") and the custom layers
      // flicker until they re-apply. One deferred apply is enough — it reads the provider
      // and theme fresh when the style actually loaded.
      if (this.styleLoadPending) return;
      this.styleLoadPending = true;
      this.map.once("style.load", () => {
        this.styleLoadPending = false;
        this.applyCurrentStyle();
      });
      return;
    }
    const theme = getThemeColors();
    if (this.provider.kind === "vector-style") {
      const url = theme.isDark ? this.provider.styleUrl?.dark : this.provider.styleUrl?.light;
      if (url) {
        const map = this.map;
        void loadRepairedStyle(url).then((style) => {
          if (this.map === map) map.setStyle(style, { diff: false });
        }).catch(() => {
          if (this.map === map) map.setStyle(url, { diff: true });
        });
        return;
      }
    }
    const style = buildMapStyle(this.provider, theme);
    this.map.setStyle(style, { diff: true });
  }

  public destroy() {
    if (this.observer) {
      this.observer.disconnect();
      this.observer = null;
    }
    this.map = null;
  }
}
