import { useEffect, useRef } from "react";
import * as maplibregl from "maplibre-gl";
import type { LngLatBounds } from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import type { Point } from "geojson";
import type { CameraEntity, MapProviderConfig, Site } from "@/lib/maps/types";
import { EntityIndex } from "@/lib/maps/entityIndex";
import { buildMapStyle, getThemeColors, MapStyleController } from "./MapStyleController";
import { buildCameraLayers, buildCamerasSource, CAMERAS_SOURCE_ID } from "./layers/cameraLayers";
import { buildSiteLayers, buildSitesSource, SITES_SOURCE_ID, sitesToFeatureCollection } from "./layers/sitesLayer";
import { registerSdfSprites } from "./sprite";

export interface MapCanvasProps {
  provider: MapProviderConfig;
  center?: [number, number]; // [lng, lat]
  zoom?: number;
  pitch?: number;
  bearing?: number;
  cameras?: CameraEntity[];
  sites?: Site[];
  selectedCameraId?: string;
  hoveredCameraId?: string;
  onSelectCamera?: (cameraId: string) => void;
  onHoverCamera?: (cameraId: string | null) => void;
  onSelectSite?: (siteId: string) => void;
  onMapReady?: (map: maplibregl.Map) => void;
  onMoveEnd?: (view: { center: [number, number]; zoom: number; bounds: LngLatBounds }) => void;
  onReapplyCustomLayers?: () => void;
  className?: string;
}

export function MapCanvas({
  provider,
  center = [-58.3816, -34.6037],
  zoom = 12,
  pitch = 0,
  bearing = 0,
  cameras = [],
  sites = [],
  selectedCameraId,
  hoveredCameraId,
  onSelectCamera,
  onHoverCamera,
  onSelectSite,
  onMapReady,
  onMoveEnd,
  onReapplyCustomLayers,
  className = "relative h-full w-full overflow-hidden",
}: MapCanvasProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const controllerRef = useRef<MapStyleController | null>(null);
  const prevSelectedIdRef = useRef<string | undefined>(undefined);
  const prevHoveredIdRef = useRef<string | undefined>(undefined);

  const onMoveEndRef = useRef(onMoveEnd);
  const onSelectCameraRef = useRef(onSelectCamera);
  const onHoverCameraRef = useRef(onHoverCamera);
  const onSelectSiteRef = useRef(onSelectSite);
  const camerasRef = useRef(cameras);
  const sitesRef = useRef(sites);

  const setupCustomLayersRef = useRef<(map: maplibregl.Map) => void>(() => {});

  useEffect(() => {
    onMoveEndRef.current = onMoveEnd;
    onSelectCameraRef.current = onSelectCamera;
    onHoverCameraRef.current = onHoverCamera;
    onSelectSiteRef.current = onSelectSite;
    camerasRef.current = cameras;
    sitesRef.current = sites;
    setupCustomLayersRef.current = (map: maplibregl.Map) => {
      registerSdfSprites(map);

      // 1. Sites Source & Layers (Overview / country level)
      if (!map.getSource(SITES_SOURCE_ID)) {
        map.addSource(SITES_SOURCE_ID, buildSitesSource(sitesRef.current));
        for (const layer of buildSiteLayers()) {
          if (!map.getLayer(layer.id)) map.addLayer(layer);
        }
      }

      // 2. Cameras Source & Layers (Clustered & Unclustered)
      if (!map.getSource(CAMERAS_SOURCE_ID)) {
        const sourceSpec = buildCamerasSource();
        const entityIndex = new EntityIndex(camerasRef.current);
        sourceSpec.data = entityIndex.toFeatureCollection();
        map.addSource(CAMERAS_SOURCE_ID, sourceSpec);
        for (const layer of buildCameraLayers()) {
          if (!map.getLayer(layer.id)) map.addLayer(layer);
        }
      }

      onReapplyCustomLayers?.();
    };
  });

  const initialOptionsRef = useRef({
    provider,
    center,
    zoom,
    pitch,
    bearing,
    onMapReady,
  });

  useEffect(() => {
    if (!containerRef.current) return;
    const {
      provider: initProvider,
      center: initCenter,
      zoom: initZoom,
      pitch: initPitch,
      bearing: initBearing,
      onMapReady: initOnMapReady,
    } = initialOptionsRef.current;

    const initialStyle =
      initProvider.kind === "vector-style" && initProvider.styleUrl
        ? getThemeColors().isDark
          ? initProvider.styleUrl.dark || buildMapStyle(initProvider)
          : initProvider.styleUrl.light || buildMapStyle(initProvider)
        : buildMapStyle(initProvider);

    const map = new maplibregl.Map({
      container: containerRef.current,
      style: initialStyle,
      center: initCenter,
      zoom: initZoom,
      pitch: initPitch,
      bearing: initBearing,
      attributionControl: false,
    });

    map.addControl(new maplibregl.NavigationControl({ showCompass: true, showZoom: true }), "top-right");
    map.addControl(new maplibregl.AttributionControl({ compact: true }), "bottom-right");

    const controller = new MapStyleController(initProvider, () => setupCustomLayersRef.current(map));
    controller.attach(map);
    controllerRef.current = controller;
    mapRef.current = map;

    map.on("load", () => {
      setupCustomLayersRef.current(map);
      initOnMapReady?.(map);
    });

    // Cluster expansion click
    map.on("click", "cam-cluster", async (e) => {
      const features = map.queryRenderedFeatures(e.point, { layers: ["cam-cluster"] });
      const first = features[0];
      if (!first) return;
      const clusterId = (first.properties as Record<string, unknown> | undefined)?.cluster_id;
      const source = map.getSource(CAMERAS_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
      if (source && typeof clusterId === "number") {
        try {
          const nextZoom = await source.getClusterExpansionZoom(clusterId);
          const geom = first.geometry as Point;
          map.easeTo({ center: geom.coordinates as [number, number], zoom: nextZoom });
        } catch {
          // Ignored
        }
      }
    });

    // Unclustered camera click
    map.on("click", "cam-point-circle", (e) => {
      const feat = e.features?.[0];
      if (feat?.properties?.id) {
        onSelectCameraRef.current?.(feat.properties.id);
      }
    });

    // Site overview click
    map.on("click", "site-point", (e) => {
      const feat = e.features?.[0];
      if (feat?.properties?.id) {
        onSelectSiteRef.current?.(feat.properties.id);
      }
    });

    // Cursor hover effects
    map.on("mouseenter", "cam-cluster", () => {
      map.getCanvas().style.cursor = "pointer";
    });
    map.on("mouseleave", "cam-cluster", () => {
      map.getCanvas().style.cursor = "";
    });

    map.on("mouseenter", "cam-point-circle", (e) => {
      map.getCanvas().style.cursor = "pointer";
      const id = e.features?.[0]?.properties?.id;
      if (id) onHoverCameraRef.current?.(id);
    });
    map.on("mouseleave", "cam-point-circle", () => {
      map.getCanvas().style.cursor = "";
      onHoverCameraRef.current?.(null);
    });

    map.on("mouseenter", "site-point", () => {
      map.getCanvas().style.cursor = "pointer";
    });
    map.on("mouseleave", "site-point", () => {
      map.getCanvas().style.cursor = "";
    });

    map.on("moveend", () => {
      if (onMoveEndRef.current) {
        const c = map.getCenter();
        onMoveEndRef.current({
          center: [c.lng, c.lat],
          zoom: map.getZoom(),
          bounds: map.getBounds(),
        });
      }
    });

    const resizeObserver = new ResizeObserver(() => {
      map.resize();
    });
    resizeObserver.observe(containerRef.current);

    return () => {
      resizeObserver.disconnect();
      controller.destroy();
      map.remove();
      mapRef.current = null;
      controllerRef.current = null;
    };
  }, []);

  // Update cameras GeoJSON data
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    const source = map.getSource(CAMERAS_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
    if (source) {
      const entityIndex = new EntityIndex(cameras);
      source.setData(entityIndex.toFeatureCollection());
    }
  }, [cameras]);

  // Update sites GeoJSON data
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    const source = map.getSource(SITES_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
    if (source) {
      source.setData(sitesToFeatureCollection(sites));
    }
  }, [sites]);

  // Update selection feature-state
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    if (prevSelectedIdRef.current && prevSelectedIdRef.current !== selectedCameraId) {
      try {
        map.setFeatureState({ source: CAMERAS_SOURCE_ID, id: prevSelectedIdRef.current }, { selected: false });
      } catch {
        // Ignored if feature disappeared
      }
    }
    if (selectedCameraId) {
      try {
        map.setFeatureState({ source: CAMERAS_SOURCE_ID, id: selectedCameraId }, { selected: true });
      } catch {
        // Ignored
      }
    }
    prevSelectedIdRef.current = selectedCameraId;
  }, [selectedCameraId]);

  // Update hover feature-state
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    if (prevHoveredIdRef.current && prevHoveredIdRef.current !== hoveredCameraId) {
      try {
        map.setFeatureState({ source: CAMERAS_SOURCE_ID, id: prevHoveredIdRef.current }, { hover: false });
      } catch {
        // Ignored
      }
    }
    if (hoveredCameraId) {
      try {
        map.setFeatureState({ source: CAMERAS_SOURCE_ID, id: hoveredCameraId }, { hover: true });
      } catch {
        // Ignored
      }
    }
    prevHoveredIdRef.current = hoveredCameraId;
  }, [hoveredCameraId]);

  // React to provider changes
  useEffect(() => {
    if (controllerRef.current) {
      controllerRef.current.setProvider(provider);
    }
  }, [provider]);

  return (
    <div
      ref={containerRef}
      className={className}
      data-testid="map-canvas-container"
      tabIndex={0}
      aria-label="Mapa operativo"
    />
  );
}
