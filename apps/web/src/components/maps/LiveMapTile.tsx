import { lazy, Suspense } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { mapsConfigQuery, siteEntitiesQuery } from "@/lib/maps/api";
import type { CameraEntity } from "@/lib/maps/types";
import type { LiveMapRef } from "@/lib/liveGrid";

const MapCanvas = lazy(() => import("./canvas/MapCanvas").then((mod) => ({ default: mod.MapCanvas })));

/** A named map inside one Live grid cell. Floor plans open the full editor; geography renders in place. */
export function LiveMapTile({ map }: { map: LiveMapRef }) {
  const geographic = !map.floor_id;
  const config = useQuery({ ...mapsConfigQuery, enabled: geographic });
  const entities = useQuery({ ...siteEntitiesQuery(map.site_id), enabled: geographic });
  const cameras = (entities.data?.entities ?? []).filter((entity): entity is CameraEntity => entity.type === "camera" && "camera" in entity && entity.position.kind === "geo");
  const first = cameras[0]?.position.kind === "geo" ? cameras[0].position : undefined;
  const center: [number, number] | undefined = first
    ? [first.lng, first.lat]
    : config.data
      ? [config.data.defaultCenter.lng, config.data.defaultCenter.lat]
      : undefined;

  return (
    <div className="relative size-full bg-bg">
      {geographic && config.data && center ? (
        <Suspense fallback={<p className="p-3 text-xs text-muted">Cargando mapa…</p>}>
          <MapCanvas provider={config.data.provider} cameras={cameras} center={center} zoom={config.data.defaultZoom} className="size-full" />
        </Suspense>
      ) : (
        <Link
          to="/maps"
          search={{ site: map.site_id, floor: map.floor_id, mode: "live" }}
          className="flex size-full flex-col items-center justify-center gap-1 p-3 text-center text-sm text-ink hover:bg-raised"
        >
          <span className="font-medium">{map.name}</span>
          <span className="text-xs text-muted">{map.floor_id ? "Abrir plano" : "Abrir mapa"}</span>
        </Link>
      )}
      <div className="pointer-events-none absolute left-2 top-2 max-w-[70%] truncate rounded bg-black/70 px-2 py-1 text-xs text-white">{map.name}</div>
    </div>
  );
}
