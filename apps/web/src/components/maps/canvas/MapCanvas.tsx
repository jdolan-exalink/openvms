import { useEffect, useRef } from "react";
import * as maplibregl from "maplibre-gl";
import type { LngLatBounds } from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import type { MapProviderConfig } from "@/lib/maps/types";
import { buildMapStyle, getThemeColors, MapStyleController } from "./MapStyleController";

export interface MapCanvasProps {
  provider: MapProviderConfig;
  center?: [number, number]; // [lng, lat]
  zoom?: number;
  pitch?: number;
  bearing?: number;
  onMapReady?: (map: maplibregl.Map) => void;
  onMoveEnd?: (view: { center: [number, number]; zoom: number; bounds: LngLatBounds }) => void;
  onReapplyCustomLayers?: () => void;
  className?: string;
}

export function MapCanvas({
  provider,
  center = [-58.3816, -34.6037], // Buenos Aires default fallback if none provided
  zoom = 12,
  pitch = 0,
  bearing = 0,
  onMapReady,
  onMoveEnd,
  onReapplyCustomLayers,
  className = "relative h-full w-full overflow-hidden",
}: MapCanvasProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const controllerRef = useRef<MapStyleController | null>(null);
  const onMoveEndRef = useRef(onMoveEnd);

  useEffect(() => {
    onMoveEndRef.current = onMoveEnd;
  }, [onMoveEnd]);

  const initialOptionsRef = useRef({
    provider,
    center,
    zoom,
    pitch,
    bearing,
    onMapReady,
    onReapplyCustomLayers,
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
      onReapplyCustomLayers: initOnReapplyCustomLayers,
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

    const controller = new MapStyleController(initProvider, initOnReapplyCustomLayers);
    controller.attach(map);
    controllerRef.current = controller;
    mapRef.current = map;

    map.on("load", () => {
      initOnMapReady?.(map);
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
  }, []); // Run once on mount

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
