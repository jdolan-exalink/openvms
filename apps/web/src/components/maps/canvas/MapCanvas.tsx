import { useEffect, useRef } from "react";
import * as maplibregl from "maplibre-gl";
import type { LngLatBounds } from "maplibre-gl";
import maplibreWorkerUrl from "maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url";

// MapLibre v6 ships its worker as a sibling file it locates relative to its own bundle
// URL — after bundling that points at /assets/maplibre-gl-worker.mjs, which no bundler
// emits. Worse, a raw copy of the file still imports ./maplibre-gl-shared.mjs, which
// Rollup inlines elsewhere. ?worker&url makes Vite bundle a self-contained worker and
// hand us its URL; worker.format "es" (vite.config) keeps it a module worker.
maplibregl.config.WORKER_URL = maplibreWorkerUrl;
import "maplibre-gl/dist/maplibre-gl.css";
import type { Point } from "geojson";
import type { CameraEntity, MapProviderConfig, Site, Zone } from "@/lib/maps/types";
import { type BoundingBox } from "@/lib/maps/geo";
import { EntityIndex } from "@/lib/maps/entityIndex";
import { buildMapStyle, getThemeColors, loadRepairedStyle, MapStyleController } from "./MapStyleController";
import { buildCamerasSource, CAMERAS_SOURCE_ID } from "./layers/cameraLayers";
import { buildFovSource, camerasToFovCollection, FOV_SOURCE_ID } from "./layers/fovLayer";
import { buildSitesSource, SITES_SOURCE_ID, sitesToFeatureCollection } from "./layers/sitesLayer";
import { buildZoneSketchLayers, buildZonesSource, sketchToFeatureCollection, ZONE_SKETCH_SOURCE_ID, zonesToFeatureCollection, ZONES_SOURCE_ID, type ZoneSketch } from "./layers/zonesLayer";
import { buildFxSource, FX_SOURCE_ID } from "./layers/fxLayers";
import { buildHeatmapSource, HEATMAP_SOURCE_ID, analyticsToFeatureCollection, type AnalyticsPoint } from "./layers/heatmapLayer";
import { reconcileOwnedLayers, applyLayerVisibility, LAYER_GROUPS, type LayerGroup } from "./layers/visibility";
import { AnimationBudget, isPrefersReducedMotion } from "@/lib/maps/animationBudget";
import { defaultMapRealtimeStore, MapRealtimeStore } from "@/lib/maps/mapRealtimeStore";
import { registerSdfSprites } from "./sprite";
import { bindCameraPointerDrag } from "@/lib/maps/editorInteractions";
import { bindLongPress, trackPointerKind } from "@/lib/touch";

export interface MapCanvasProps {
  provider: MapProviderConfig;
  center?: [number, number]; // [lng, lat]
  zoom?: number;
  pitch?: number;
  bearing?: number;
  cameras?: CameraEntity[];
  sites?: Site[];
  zones?: Zone[];
  coverage?: boolean;
  /** Per-group visibility driven by the user's saved layer preferences. */
  layerVisibility?: Partial<Record<LayerGroup, boolean>>;
  selectedCameraId?: string;
  hoveredCameraId?: string;
  realtimeStore?: MapRealtimeStore;
  animationBudget?: AnimationBudget;
  onSelectCamera?: (cameraId: string) => void;
  onHoverCamera?: (cameraId: string | null, point?: { x: number; y: number }) => void;
  onDoubleClickCamera?: (cameraId: string) => void;
  onContextMenuCamera?: (cameraId: string, point: { x: number; y: number }) => void;
  onSelectSite?: (siteId: string) => void;
  /** Clicks that did not land on any feature — the editor places/moves on these. */
  onMapClick?: (point: { lng: number; lat: number }) => void;
  /** While a zone is open, every map click adds a vertex, including clicks on a camera. */
  drawingZone?: boolean;
  /** Vertices of the zone currently being drawn. */
  zoneSketch?: ZoneSketch;
  onCameraDragStart?: (cameraId: string) => void;
  onCameraDragMove?: (cameraId: string, point: { lng: number; lat: number }) => void;
  onCameraDragEnd?: (cameraId: string, point: { lng: number; lat: number }) => void;
  onMapReady?: (map: maplibregl.Map) => void;
  onMoveEnd?: (view: { center: [number, number]; zoom: number; bounds: LngLatBounds }) => void;
  onReapplyCustomLayers?: () => void;
  /** Edit mode keeps every camera separate so markers can be dragged at any zoom. */
  clusterCameras?: boolean;
  /** Activity heatmap points to render on the map. */
  heatmapPoints?: AnalyticsPoint[];
  /** When true, dims camera layers and disables fx for heatmap clarity. */
  analyticsMode?: boolean;
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
  zones = [],
  coverage = true,
  layerVisibility,
  selectedCameraId,
  hoveredCameraId,
  realtimeStore,
  animationBudget,
  onSelectCamera,
  onHoverCamera,
  onDoubleClickCamera,
  onContextMenuCamera,
  onSelectSite,
  onMapClick,
  drawingZone = false,
  zoneSketch,
  onCameraDragStart,
  onCameraDragMove,
  onCameraDragEnd,
  onMapReady,
  onMoveEnd,
  onReapplyCustomLayers,
  clusterCameras = true,
  heatmapPoints = [],
  analyticsMode = false,
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
  const onMapClickRef = useRef(onMapClick);
  const drawingZoneRef = useRef(drawingZone);
  const zoneSketchRef = useRef(zoneSketch);
  const onCameraDragStartRef = useRef(onCameraDragStart);
  const onCameraDragMoveRef = useRef(onCameraDragMove);
  const onCameraDragEndRef = useRef(onCameraDragEnd);
  const onHoverCameraRef = useRef(onHoverCamera);
  const onDoubleClickCameraRef = useRef(onDoubleClickCamera);
  const onContextMenuCameraRef = useRef(onContextMenuCamera);
  const onSelectSiteRef = useRef(onSelectSite);
  const camerasRef = useRef(cameras);
  const sitesRef = useRef(sites);
  const zonesRef = useRef(zones);
  const coverageRef = useRef(coverage);
  const layerVisibilityRef = useRef(layerVisibility);
  const clusterRef = useRef(clusterCameras);
  clusterRef.current = clusterCameras;
  const installedClusterRef = useRef<boolean | null>(null);
  const heatmapPointsRef = useRef(heatmapPoints);
  const analyticsModeRef = useRef(analyticsMode);

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
    onDoubleClickCameraRef.current = onDoubleClickCamera;
    onContextMenuCameraRef.current = onContextMenuCamera;
    onSelectSiteRef.current = onSelectSite;
    onMapClickRef.current = onMapClick;
    drawingZoneRef.current = drawingZone;
    zoneSketchRef.current = zoneSketch;
    onCameraDragStartRef.current = onCameraDragStart;
    onCameraDragMoveRef.current = onCameraDragMove;
    onCameraDragEndRef.current = onCameraDragEnd;
    camerasRef.current = cameras;
    sitesRef.current = sites;
    zonesRef.current = zones;
    coverageRef.current = coverage;
    layerVisibilityRef.current = layerVisibility;
    heatmapPointsRef.current = heatmapPoints;
    analyticsModeRef.current = analyticsMode;

    setupCustomLayersRef.current = (map: maplibregl.Map) => {
      registerSdfSprites(map);

      // 1. Sites Source & Layers (Country / Overview level)
      if (!map.getSource(SITES_SOURCE_ID)) {
        map.addSource(SITES_SOURCE_ID, buildSitesSource(sitesRef.current));
      }

      // 1b. Zones Source & Layers (site polygons, underneath every device layer)
      if (!map.getSource(ZONES_SOURCE_ID)) {
        map.addSource(ZONES_SOURCE_ID, buildZonesSource(zonesRef.current));
      }
      if (!map.getSource(ZONE_SKETCH_SOURCE_ID)) {
        map.addSource(ZONE_SKETCH_SOURCE_ID, { type: "geojson", data: sketchToFeatureCollection(zoneSketchRef.current) });
      }

      // 1c. Heatmap Source & Layers (underneath FOV and cameras)
      if (!map.getSource(HEATMAP_SOURCE_ID)) {
        map.addSource(HEATMAP_SOURCE_ID, buildHeatmapSource(heatmapPointsRef.current));
      }

      // 2. FOV Cones Source & Layers (Street level, rendered underneath cameras)
      if (!map.getSource(FOV_SOURCE_ID)) {
        map.addSource(FOV_SOURCE_ID, buildFovSource());
      }

      // 3. Cameras Source & Layers (Clustered & Unclustered)
      if (!map.getSource(CAMERAS_SOURCE_ID)) {
        const sourceSpec = buildCamerasSource({ cluster: clusterRef.current });
        installedClusterRef.current = clusterRef.current;
        const store = realtimeStore ?? defaultMapRealtimeStore;
        const { cameras: patched } = store.patchCameras(camerasRef.current);
        const entityIndex = new EntityIndex(patched);
        sourceSpec.data = entityIndex.toFeatureCollection();
        map.addSource(CAMERAS_SOURCE_ID, sourceSpec);
      }

      // 4. FX Source & Layers (Ripples & Alarm Pulses, rendered on top of cameras)
      if (!map.getSource(FX_SOURCE_ID)) {
        map.addSource(FX_SOURCE_ID, buildFxSource());
      }

      reconcileOwnedLayers(map, coverageRef.current);

      // The style can be rebuilt at any moment (theme swap, provider change), so the user's
      // layer preferences have to be re-applied every time these layers come back.
      applyLayerVisibility(map, layerVisibilityRef.current ?? {});
      for (const layer of buildZoneSketchLayers()) {
        if (!map.getLayer(layer.id)) map.addLayer(layer);
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

    const hostedStyle =
      initProvider.kind === "vector-style" && initProvider.styleUrl
        ? getThemeColors().isDark
          ? initProvider.styleUrl.dark
          : initProvider.styleUrl.light
        : undefined;

    const map = new maplibregl.Map({
      container: containerRef.current,
      style: hostedStyle
        ? { version: 8, sources: {}, layers: [{ id: "background", type: "background", paint: { "background-color": getThemeColors().background } }] }
        : buildMapStyle(initProvider),
      center: initCenter,
      zoom: initZoom,
      pitch: initPitch,
      bearing: initBearing,
      attributionControl: false,
    });

    map.on("styleimagemissing", (event) => {
      if (!map.hasImage(event.id)) map.addImage(event.id, { width: 1, height: 1, data: new Uint8Array(4) });
    });
    if (hostedStyle) {
      void loadRepairedStyle(hostedStyle).then((style) => {
        if (mapRef.current === map) map.setStyle(style);
      }).catch(() => {
        if (mapRef.current === map) map.setStyle(hostedStyle);
      });
    }

    map.addControl(new maplibregl.NavigationControl({ showCompass: true, showZoom: true }), "top-right");

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

    // Touch: a finger emulates hover, so the hover preview ignores it and a tap opens the preview
    // instead; a long press opens the camera context menu (right click on a mouse).
    const pointerKind = trackPointerKind(map.getCanvas());
    const longPress = bindLongPress(map.getCanvas(), ({ x, y }) => {
      if (drawingZoneRef.current || !map.getLayer("cam-point-circle")) return;
      const rect = map.getCanvas().getBoundingClientRect();
      const point = { x: x - rect.left, y: y - rect.top };
      const id = map.queryRenderedFeatures([point.x, point.y], { layers: ["cam-point-circle"] })[0]?.properties?.id;
      if (id) onContextMenuCameraRef.current?.(String(id), point);
    });

    // Unclustered camera click
    map.on("click", "cam-point-circle", (e) => {
      if (drawingZoneRef.current) return;
      if (longPress.consumeFired()) return;
      if (cameraDrag.consumeClick()) return;
      const feat = e.features?.[0];
      if (feat?.properties?.id) {
        onSelectCameraRef.current?.(feat.properties.id);
        if (pointerKind.isTouch()) onHoverCameraRef.current?.(feat.properties.id, { x: e.point.x, y: e.point.y });
      }
    });
    // Tapping empty map dismisses a preview a tap opened.
    map.on("click", (e) => {
      if (!pointerKind.isTouch() || !map.getLayer("cam-point-circle")) return;
      if (map.queryRenderedFeatures(e.point, { layers: ["cam-point-circle"] }).length === 0) onHoverCameraRef.current?.(null);
    });

    const cameraDrag = bindCameraPointerDrag(map.getCanvas(), {
      enabled: () => !!onCameraDragStartRef.current && !!onCameraDragMoveRef.current,
      pick: point => {
        if (!map.getLayer("cam-point-circle")) return;
        const id = map.queryRenderedFeatures(point, { layers: ["cam-point-circle"] })[0]?.properties?.id;
        return id ? String(id) : undefined;
      },
      pan: map.dragPan,
      unproject: point => map.unproject(point),
      start: id => onCameraDragStartRef.current?.(id),
      move: (id, point) => onCameraDragMoveRef.current?.(id, point),
      end: (id, point) => onCameraDragEndRef.current?.(id, point),
    });

    // Site overview click
    map.on("click", "site-point", (e) => {
      const feat = e.features?.[0];
      if (feat?.properties?.id) {
        onSelectSiteRef.current?.(feat.properties.id);
      }
    });

    // Empty-space click: only the editor stages placements on these, so the feature
    // handlers above keep owning everything the operator clicked on purpose.
    map.on("click", (e) => {
      if (!onMapClickRef.current) return;
      if (drawingZoneRef.current) {
        onMapClickRef.current({ lng: e.lngLat.lng, lat: e.lngLat.lat });
        return;
      }
      const featureLayers = ["cam-cluster", "cam-point-circle", "site-point"].filter((id) => map.getLayer(id));
      const hits = featureLayers.length
        ? map.queryRenderedFeatures(e.point, { layers: featureLayers })
        : [];
      if (hits.length > 0) return;
      onMapClickRef.current({ lng: e.lngLat.lng, lat: e.lngLat.lat });
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
      if (pointerKind.isTouch()) return;
      const id = e.features?.[0]?.properties?.id;
      if (id) onHoverCameraRef.current?.(id, { x: e.point.x, y: e.point.y });
    });
    map.on("mousemove", "cam-point-circle", (e) => {
      if (pointerKind.isTouch()) return;
      const id = e.features?.[0]?.properties?.id;
      if (id) onHoverCameraRef.current?.(id, { x: e.point.x, y: e.point.y });
    });
    map.on("mouseleave", "cam-point-circle", () => {
      map.getCanvas().style.cursor = "";
      if (pointerKind.isTouch()) return;
      onHoverCameraRef.current?.(null);
    });

    map.on("dblclick", "cam-point-circle", (e) => {
      e.preventDefault();
      const id = e.features?.[0]?.properties?.id;
      if (id) onDoubleClickCameraRef.current?.(id);
    });

    map.on("contextmenu", "cam-point-circle", (e) => {
      e.preventDefault();
      const id = e.features?.[0]?.properties?.id;
      if (id) onContextMenuCameraRef.current?.(id, { x: e.point.x, y: e.point.y });
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
      cameraDrag.dispose();
      longPress.dispose();
      pointerKind.dispose();
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

  // Clustering is fixed when the source is created. Edit mode rebuilds it without clusters
  // so a zoomed-out site still exposes every marker.
  useEffect(() => {
    const map = mapRef.current;
    if (!map?.isStyleLoaded() || !map.getSource(CAMERAS_SOURCE_ID)) return;
    if (installedClusterRef.current === clusterCameras) return;
    for (const id of LAYER_GROUPS.cameras) {
      if (map.getLayer(id)) map.removeLayer(id);
    }
    map.removeSource(CAMERAS_SOURCE_ID);
    const spec = buildCamerasSource({ cluster: clusterCameras });
    const store = realtimeStore ?? defaultMapRealtimeStore;
    const { cameras: patched } = store.patchCameras(camerasRef.current);
    spec.data = new EntityIndex(patched).toFeatureCollection();
    map.addSource(CAMERAS_SOURCE_ID, spec);
    installedClusterRef.current = clusterCameras;
    reconcileOwnedLayers(map, coverageRef.current);
    applyLayerVisibility(map, layerVisibilityRef.current ?? {});
  }, [clusterCameras, realtimeStore]);

  // Update sites GeoJSON data
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    const source = map.getSource(SITES_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
    if (source) {
      source.setData(sitesToFeatureCollection(sites));
    }
  }, [sites]);

  // Update zones GeoJSON data
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    const source = map.getSource(ZONES_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
    if (source) {
      source.setData(zonesToFeatureCollection(zones));
    }
  }, [zones]);

  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    const source = map.getSource(ZONE_SKETCH_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
    source?.setData(sketchToFeatureCollection(zoneSketch));
  }, [zoneSketch]);

  // Update heatmap GeoJSON data
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    const source = map.getSource(HEATMAP_SOURCE_ID) as maplibregl.GeoJSONSource | undefined;
    if (source) {
      source.setData(analyticsToFeatureCollection(heatmapPoints));
    }
  }, [heatmapPoints]);

  // Dim cameras and hide fx when in analytics mode
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    if (analyticsMode) {
      if (map.getLayer("cam-point-circle")) {
        map.setPaintProperty("cam-point-circle", "circle-opacity", 0.4);
      }
      if (map.getLayer("cam-cluster")) {
        map.setPaintProperty("cam-cluster", "circle-opacity", 0.4);
      }
      if (map.getLayer("fx-ripple")) {
        map.setLayoutProperty("fx-ripple", "visibility", "none");
      }
      if (map.getLayer("fx-alarm-pulse")) {
        map.setLayoutProperty("fx-alarm-pulse", "visibility", "none");
      }
    } else {
      if (map.getLayer("cam-point-circle")) {
        map.setPaintProperty("cam-point-circle", "circle-opacity", 1);
      }
      if (map.getLayer("cam-cluster")) {
        map.setPaintProperty("cam-cluster", "circle-opacity", 1);
      }
      const vis = layerVisibilityRef.current ?? {};
      if (map.getLayer("fx-ripple")) {
        map.setLayoutProperty("fx-ripple", "visibility", vis.detectionFx !== false ? "visible" : "none");
      }
      if (map.getLayer("fx-alarm-pulse")) {
        map.setLayoutProperty("fx-alarm-pulse", "visibility", vis.alarmFx !== false ? "visible" : "none");
      }
    }
  }, [analyticsMode]);

  // Toggle layer-group visibility from the saved preferences
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !map.isStyleLoaded()) return;
    applyLayerVisibility(map, layerVisibility ?? {});
  }, [layerVisibility]);

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
