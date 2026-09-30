import { useQuery } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { meQuery } from "@/api/queries";
import { can } from "@/lib/perm";
import { MapShell } from "@/components/maps/MapShell";
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
  const search = useSearch({ from: "/app/maps" as never }) as MapsSearch;
  const me = useQuery(meQuery);

  const canView = can(me.data, "maps.view");
  const canEdit = can(me.data, "maps.edit") || can(me.data, "maps.edit_device");

  if (me.data && !canView) {
    return (
      <div className="flex h-full w-full items-center justify-center p-6 bg-bg">
        <div className="flex max-w-md flex-col items-center gap-3 text-center">
          <div className="flex size-12 items-center justify-center rounded-full bg-bad/10 text-bad">
            <ShieldAlert className="size-6" aria-hidden />
          </div>
          <h2 className="text-lg font-semibold text-ink">Acceso restringido</h2>
          <p className="text-sm text-muted">
            No tienes los permisos necesarios (<code className="font-mono text-xs">maps.view</code>) para acceder a los
            mapas geoespaciales. Contacta a un administrador.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="relative h-[calc(100vh-3.5rem)] w-full overflow-hidden">
      <MapShell
        initialSiteId={search.site}
        initialCameraId={search.camera}
        initialMode={search.mode || "live"}
        canEdit={canEdit}
      />
    </div>
  );
}

export default Maps;
