import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { meQuery } from "@/api/queries";
import { can } from "@/lib/perm";
import { MapWorkspace } from "@/components/maps/MapWorkspace";
import type { MapMode } from "@/lib/maps/types";
import { ShieldAlert } from "lucide-react";

export type MapsSearch = {
  site?: string;
  camera?: string;
  mode?: MapMode;
  alarm?: string;
  floor?: string;
};

export function Maps() {
  const search = useSearch({ strict: false }) as MapsSearch;
  const navigate = useNavigate();
  const me = useQuery(meQuery);

  const canView = can(me.data, "maps.view");
  const canEdit = can(me.data, "maps.edit") || can(me.data, "maps.edit_device");

  if (me.data && !canView) {
    return (
      <div className="flex h-full w-full items-center justify-center p-6 bg-bg">
        <div className="flex max-w-md flex-col items-center gap-3 rounded-m3-xl bg-surface-1 p-8 text-center">
          <div className="flex size-14 items-center justify-center rounded-full bg-bad/15 text-bad">
            <ShieldAlert className="size-6" aria-hidden />
          </div>
          <h2 className="text-2xl font-extrabold text-on-surface">Acceso restringido</h2>
          <p className="text-sm text-on-surface-variant">
            No tienes los permisos necesarios (<code className="font-mono text-xs">maps.view</code>) para acceder a los
            mapas geoespaciales. Contacta a un administrador.
          </p>
        </div>
      </div>
    );
  }

  const handleSelectSite = (siteId: string | undefined) => {
    void navigate({
      to: "/maps",
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      search: (prev: any) => ({
        ...prev,
        site: siteId,
        camera: undefined,
        floor: undefined,
      }),

    });
  };

  const handleSelectCamera = (cameraId: string | undefined) => {
    void navigate({
      to: "/maps",
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      search: (prev: any) => ({
        ...prev,
        camera: cameraId,
      }),

    });
  };

  const handleModeChange = (mode: MapMode) => {
    void navigate({
      to: "/maps",
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      search: (prev: any) => ({
        ...prev,
        mode,
      }),

    });
  };

  return (
    <div className="relative flex min-h-0 w-full flex-1 flex-col overflow-hidden">
      <MapWorkspace
        initialSiteId={search.site}
        initialFloorId={search.floor}
        onSelectMap={(site,floor) => void navigate({to:"/maps",search:prev=>({...prev,mode:search.mode,site,floor,camera:undefined})})}
        initialCameraId={search.camera}
        initialMode={search.mode || "live"}
        canEdit={canEdit}
        onSelectSite={handleSelectSite}
        onSelectCamera={handleSelectCamera}
        onModeChange={handleModeChange}
      />
    </div>
  );
}

export default Maps;
