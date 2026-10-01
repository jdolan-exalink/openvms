import { api, unwrap } from "@/api/client";

/**
 * saveSiteMonitoringCenter pins a site's monitoring center: the position the map lands on
 * when the site opens plus the landing zoom. The endpoint is the SiteGeo PATCH from M-B3
 * and demands maps.edit on the site.
 */
export async function saveSiteMonitoringCenter(
  siteId: string,
  lat: number,
  lng: number,
  defaultZoom: number,
): Promise<void> {
  unwrap(
    await api.PATCH("/api/v1/sites/{siteId}/geo", {
      params: { path: { siteId } },
      body: { lat, lng, default_zoom: defaultZoom },
    }),
  );
}
