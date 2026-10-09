import { lazy, Suspense } from "react";
import { useT } from "@/i18n";
import { useQuery } from "@tanstack/react-query";
import { meQuery } from "@/api/queries";
import { mapsConfigQuery, siteDetailQuery, siteEntitiesQuery } from "@/lib/maps/api";
import { floorEntitiesQuery } from "@/lib/maps/floorEditor";
import { frameCameras, loadGeoView, loadPlanView, MAP_CELL_PADDING, saveGeoView, savePlanView } from "@/lib/maps/mapView";
import { loadFloorPlan } from "@/lib/maps/plans";
import type { CameraEntity } from "@/lib/maps/types";
import type { LiveMapRef } from "@/lib/liveGrid";
import { FloorPlanCanvas } from "./canvas/FloorPlanCanvas";

const MapCanvas = lazy(() => import("./canvas/MapCanvas").then((mod) => ({ default: mod.MapCanvas })));

/** A named map inside one Live grid cell. Geography and floor plans both render in place. */
export function LiveMapTile({ map }: { map: LiveMapRef }) {
  return map.floor_id ? <LiveFloorTile map={map} /> : <LiveGeoTile map={map} />;
}

function LiveGeoTile({ map }: { map: LiveMapRef }) {
  const t = useT();
  const me = useQuery(meQuery);
  const config = useQuery(mapsConfigQuery);
  const entities = useQuery(siteEntitiesQuery(map.site_id));
  const cameras = (entities.data?.entities ?? []).filter((entity): entity is CameraEntity => entity.type === "camera" && "camera" in entity && entity.position.kind === "geo");
  const points = cameras.flatMap((camera) => camera.position.kind === "geo" ? [{ lng: camera.position.lng, lat: camera.position.lat }] : []);
  const saved = me.data ? loadGeoView(me.data.tenant_id, me.data.id, map.site_id) : null;
  const framed = saved ?? frameCameras(points, { width: 960, height: 540 }, MAP_CELL_PADDING);
  const first = points[0];
  const center = framed?.center ?? (first ? [first.lng, first.lat] as [number, number] : config.data ? [config.data.defaultCenter.lng, config.data.defaultCenter.lat] as [number, number] : undefined);
  const zoom = framed?.zoom ?? config.data?.defaultZoom ?? 14;
  const ready = !!config.data && !!center && !me.isLoading && !entities.isLoading;

  return (
    <div className="relative size-full bg-bg">
      {ready && config.data && center ? (
        <Suspense fallback={<p className="p-3 text-xs text-on-surface-variant">{t("maps.mapLoading")}</p>}>
          <MapCanvas
            provider={config.data.provider}
            cameras={cameras}
            center={center}
            zoom={zoom}
            className="size-full"
            onMoveEnd={(view) => {
              if (!me.data) return;
              saveGeoView(me.data.tenant_id, me.data.id, map.site_id, { center: view.center, zoom: view.zoom });
            }}
          />
        </Suspense>
      ) : (
        <p className="p-3 text-xs text-on-surface-variant">{entities.isError || config.isError ? t("maps.mapOpenFailed") : t("maps.mapLoading")}</p>
      )}
      <TileName name={map.name} />
    </div>
  );
}

function LiveFloorTile({ map }: { map: LiveMapRef }) {
  const t = useT();
  const floorId = map.floor_id ?? "";
  const me = useQuery(meQuery);
  const detail = useQuery(siteDetailQuery(map.site_id));
  const floor = detail.data?.buildings.flatMap((building) => building.floors).find((item) => item.id === floorId);
  const entities = useQuery({ ...floorEntitiesQuery(map.site_id, floorId), enabled: !!floorId });
  const plan = useQuery({
    queryKey: ["maps", "private-plan", map.site_id, floorId, floor?.plan?.widthPx ?? 0, floor?.plan?.heightPx ?? 0],
    enabled: !!floor?.plan,
    queryFn: ({ signal }) => loadFloorPlan(map.site_id, floorId, signal),
    retry: false,
  });
  const cameras = (entities.data ?? []).filter((entity): entity is CameraEntity => entity.type === "camera");
  const remembered = me.data ? loadPlanView(me.data.tenant_id, me.data.id, floorId) : null;

  return (
    <div className="relative size-full bg-bg">
      {floor?.plan && plan.data && !me.isLoading ? (
        <FloorPlanCanvas
          imageBlob={plan.data}
          width={floor.plan.widthPx || 1000}
          height={floor.plan.heightPx || 1000}
          cameras={cameras}
          editable={false}
          onPlace={() => {}}
          onSelect={() => {}}
          initialView={remembered ?? undefined}
          onViewChange={(view) => {
            if (me.data) savePlanView(me.data.tenant_id, me.data.id, floorId, view);
          }}
          tenantId={me.data?.tenant_id}
          siteId={map.site_id}
          floorId={floorId}
        />
      ) : (
        <p className="flex size-full items-center justify-center p-3 text-center text-xs text-on-surface-variant">
          {plan.isError ? t("maps.planOpenFailed") : me.isLoading || detail.isLoading || plan.isLoading ? t("maps.planLoading") : t("maps.planMissing")}
        </p>
      )}
      <TileName name={map.name} />
    </div>
  );
}

function TileName({ name }: { name: string }) {
  return <div className="pointer-events-none absolute left-2 top-2 z-20 max-w-[70%] truncate rounded-full bg-scrim px-3 py-1 text-xs font-bold text-on-surface backdrop-blur">{name}</div>;
}
