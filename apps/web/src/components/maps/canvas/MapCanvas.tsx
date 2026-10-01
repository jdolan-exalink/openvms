import { useEffect, useRef } from "react";
import * as maplibregl from "maplibre-gl";
import type { LngLatBounds } from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import type { Point } from "geojson";
import type { CameraEntity, MapProviderConfig, Site } from "@/lib/maps/types";
import { type BoundingBox } from "@/lib/maps/geo";
import { EntityIndex } from "@/lib/maps/entityIndex";
import { buildMapStyle, getThemeColors, MapStyleController } from "./MapStyleController";
import { buildCameraLayers, buildCamerasSource, CAMERAS_SOURCE_ID } from "./layers/cameraLayers";
import { buildFovLayers, buildFovSource, camerasToFovCollection, FOV_SOURCE_ID } from "./layers/fovLayer";
import { buildSiteLayers, buildSitesSource, SITES_SOURCE_ID, sitesToFeatureCollection } from "./layers/sitesLayer";
import { buildFxLayers, buildFxSource, FX_SOURCE_ID } from "./layers/fxLayers";
import { AnimationBudget, isPrefersReducedMotion } from "@/lib/maps/animationBudget";
import { defaultMapRealtimeStore, MapRealtimeStore } from "@/lib/maps/mapRealtimeStore";
import { registerSdfSprites } from "./sprite";

export interface MapCanvasProps {
  provider: MapProviderConfig;
  center?: [number, number]; // [lng, lat]
  zoom?: number;
  pitch?: number;
  bearing?: number;
  cameras?: CameraEntity[];
  sites?: Site[];
  coverage?: boolean;
  selectedCameraId?: string;
  hoveredCameraId?: string;
  realtimeStore?: MapRealtimeStore;
  animationBudget?: AnimationBudget;
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
  coverage = true,
  selectedCameraId,
  hoveredCameraId,
  realtimeStore,
  animationBudget,
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
  const fovDebounceTimerRef = useRef<NodeJS.Timeout | null>(null);
  const budgetRef = useRef<AnimationBudget>(animationBudget ?? new AnimationBudget());

  const onMoveEndRef = useRef(onMoveEnd);
  const onSelectCameraRef = useRef(onSelectCamera);
  const onHoverCameraRef = useRef(onHoverCamera);
  const onSelectSiteRef = useRef(onSelectSite);
  const camerasRef = useRef(cameras);
  const sitesRef = useRef(sites);
  const coverageRef = useRef(coverage);

  const setupCustomLayersRef = useRef<(map: maplibregl.Map) => void>(() => {});

  useEffect(() => {
    if (animationBudget) {
      budgetRef.current = animationBudget;
    }
  }, [animationBudget]);

  useEffect(() => {
    onMoveEndRef.current = onMoveEnd;
    onSelectCameraRef.current = onSelectCamera;
    onHoverCameraRef.current = onHoverCamera;
    onSelectSiteRef.current = onSelectSite;
    camerasRef.current = cameras;
    sitesRef.current = sites;
    coverageRef.current = coverage;

    setupCustomLayersRef.current = (map: maplibregl.Map) => {
      registerSdfSprites(map);

      // 1. Sites Source & Layers (Country / Overview level)
      if (!map.getSource(SITES_SOURCE_ID)) {
        map.addSource(SITES_SOURCE_ID, buildSitesSource(sitesRef.current));
        for (const layer of buildSiteLayers()) {
          if (!map.getLayer(layer.id)) map.addLayer(layer);
        }
      }

      // 2. FOV Cones Source & Layers (Street level, rendered underneath cameras)
      if (!map.getSource(FOV_SOURCE_ID)) {
        map.addSource(FOV_SOURCE_ID, buildFovSource());
        for (const layer of buildFovLayers(coverageRef.current)) {
          if (!map.getLayer(layer.id)) map.addLayer(layer);
        }
      }

      // 3. Cameras Source & Layers (Clustered & Unclustered)
      if (!map.getSource(CAMERAS_SOURCE_ID)) {
        const sourceSpec = buildCamerasSource();
        const store = realtimeStore ?? defaultMapRealtimeStore;
        const { cameras: patched } = store.patchCameras(camerasRef.current);
        const entityIndex = new EntityIndex(patched);
        sourceSpec.data = entityIndex.toFeatureCollection();
        map.addSource(CAMERAS_SOURCE_ID, sourceSpec);
        for (const layer of buildCameraLayers()) {
          if (!map.getLayer(layer.id)) map.addLayer(layer);
        }
      }

      // 4. FX Source & Layers (Ripples & Alarm Pulses, rendered on top of cameras)
      if (!map.getSource(FX_SOURCE_ID)) {
        map.addSource(FX_SOURCE_ID, buildFxSource());
        for (const layer of buildFxLayers()) {
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

    const updateFov = () => {
      const fovSource = map.getSource(FOV_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
      if (!fovSource) return;
      if (map.getZoom() < 13 || !coverageRef.current) {
        fovSource.setData({ type: "FeatureCollection", features: [] });
        return;
      }
      const b = map.getBounds();
      const bbox: BoundingBox = {
        west: b.getWest(),
        south: b.getSouth(),
        east: b.getEast(),
        north: b.getNorth(),
      };
      fovSource.setData(camerasToFovCollection(camerasRef.current, bbox));
    };

    map.on("load", () => {
      setupCustomLayersRef.current(map);
      updateFov();
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
      // Debounce FOV cone calculation by 150ms per architecture §11
      if (fovDebounceTimerRef.current) {
        clearTimeout(fovDebounceTimerRef.current);
      }
      fovDebounceTimerRef.current = setTimeout(() => {
        updateFov();
      }, 150);

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
      if (fovDebounceTimerRef.current) clearTimeout(fovDebounceTimerRef.current);
      resizeObserver.disconnect();
      controller.destroy();
      map.remove();
      mapRef.current = null;
      controllerRef.current = null;
    };
  }, []);

  // FX animation loop driven by rAF
  useEffect(() => {
    let animFrameId: number;

    const frameLoop = () => {
      const map = mapRef.current;
      const budget = budgetRef.current;

      if (!document.hidden && map && map.isStyleLoaded()) {
        const fxSource = map.getSource(FX_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
        if (fxSource && budget.hasActiveFx()) {
          const fc = budget.tick(Date.now(), isPrefersReducedMotion());
          fxSource.setData(fc);
        }
      }
      animFrameId = requestAnimationFrame(frameLoop);
    };

    animFrameId = requestAnimationFrame(frameLoop);

    return () => {
      cancelAnimationFrame(animFrameId);
    };
  }, []);

  // Listen to realtime events to trigger ripples and pulses
  useEffect(() => {
    const store = realtimeStore ?? defaultMapRealtimeStore;
    const budget = budgetRef.current;

    const unsubscribe = store.subscribe(() => {
      const recent = store.getRecentEvents(1);
      const latest = recent[0];
      if (!latest || !latest.cameraId) return;

      const cam = camerasRef.current.find((c) => c.id === latest.cameraId);
      if (!cam || cam.position.kind !== "geo") return;

      const { lng, lat } = cam.position;

      if (latest.type === "event.created") {
        const data = latest.data as { labels?: string[] } | undefined;
        const isLpr = data?.labels?.some((l) => l.includes("plate") || l.includes("license"));
        budget.addRipple({
          id: latest.id,
          lng,
          lat,
          type: isLpr ? "lpr" : "detection",
        });
      } else if (latest.type === "alarm.created") {
        budget.addRipple({
          id: latest.id,
          lng,
          lat,
          type: "alarm",
        });
        budget.setAlarmPulse({
          cameraId: cam.id,
          lng,
          lat,
          acknowledged: false,
        });
      } else if (latest.type === "alarm.acknowledged") {
        budget.setAlarmPulse({
          cameraId: cam.id,
          lng,
          lat,
          acknowledged: true,
        });
      } else if (latest.type === "alarm.updated") {
        const data = latest.data as { status?: string } | undefined;
        if (data?.status === "resolved" || data?.status === "closed") {
          budget.removeAlarmPulse(cam.id);
        }
      }
    });

    return unsubscribe;
  }, [realtimeStore]);

  // Re-sync cameras when realtime store flushes camera status/alarm changes
  useEffect(() => {
    const map = mapRef.current;
    const store = realtimeStore ?? defaultMapRealtimeStore;

    const updateCameras = () => {
      if (!map || !map.isStyleLoaded()) return;
      const source = map.getSource(CAMERAS_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
      if (!source) return;

      const { cameras: patched } = store.patchCameras(camerasRef.current);
      const index = new EntityIndex(patched);
      source.setData(index.toFeatureCollection());
    };

    return store.onCameraChange(updateCameras);
  }, [realtimeStore]);

  // Update cameras GeoJSON data
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    const source = map.getSource(CAMERAS_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
    const store = realtimeStore ?? defaultMapRealtimeStore;
    const { cameras: patched } = store.patchCameras(cameras);
    if (source) {
      const entityIndex = new EntityIndex(patched);
      source.setData(entityIndex.toFeatureCollection());
    }
    const fovSource = map.getSource(FOV_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
    if (fovSource && map.getZoom() >= 13 && coverage) {
      const b = map.getBounds();
      const bbox: BoundingBox = {
        west: b.getWest(),
        south: b.getSouth(),
        east: b.getEast(),
        north: b.getNorth(),
      };
      fovSource.setData(camerasToFovCollection(patched, bbox));
    }
  }, [cameras, coverage, realtimeStore]);

  // Update sites GeoJSON data
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    const source = map.getSource(SITES_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
    if (source) {
      source.setData(sitesToFeatureCollection(sites));
    }
  }, [sites]);

  // Toggle coverage visibility
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    const visibility = coverage ? "visible" : "none";
    if (map.getLayer("fov-fill")) {
      map.setLayoutProperty("fov-fill", "visibility", visibility);
    }
    if (map.getLayer("fov-outline")) {
      map.setLayoutProperty("fov-outline", "visibility", visibility);
    }
  }, [coverage]);

  // Update selection feature-state on cameras & fov
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;

    if (prevSelectedIdRef.current && prevSelectedIdRef.current !== selectedCameraId) {
      try {
        map.setFeatureState({ source: CAMERAS_SOURCE_ID, id: prevSelectedIdRef.current }, { selected: false });
        map.setFeatureState({ source: FOV_SOURCE_ID, id: prevSelectedIdRef.current }, { selected: false });
      } catch {
        // Ignored
      }
    }
    if (selectedCameraId) {
      try {
        map.setFeatureState({ source: CAMERAS_SOURCE_ID, id: selectedCameraId }, { selected: true });
        map.setFeatureState({ source: FOV_SOURCE_ID, id: selectedCameraId }, { selected: true });

        // Fly to camera position smoothly
        const cam = cameras.find((c) => c.id === selectedCameraId);
        if (cam && cam.position.kind === "geo") {
          map.easeTo({
            center: [cam.position.lng, cam.position.lat],
            zoom: Math.max(map.getZoom(), 16),
          });
        }
      } catch {
        // Ignored
      }
    }
    prevSelectedIdRef.current = selectedCameraId;
  }, [selectedCameraId, cameras]);

  // Update hover feature-state on cameras & fov
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;

    if (prevHoveredIdRef.current && prevHoveredIdRef.current !== hoveredCameraId) {
      try {
        map.setFeatureState({ source: CAMERAS_SOURCE_ID, id: prevHoveredIdRef.current }, { hover: false });
        map.setFeatureState({ source: FOV_SOURCE_ID, id: prevHoveredIdRef.current }, { hover: false });
      } catch {
        // Ignored
      }
    }
    if (hoveredCameraId) {
      try {
        map.setFeatureState({ source: CAMERAS_SOURCE_ID, id: hoveredCameraId }, { hover: true });
        map.setFeatureState({ source: FOV_SOURCE_ID, id: hoveredCameraId }, { hover: true });
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
