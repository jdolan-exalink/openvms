import { api, unwrap, type Schemas } from "@/api/client";

/**
 * importPlacements posts the operator's CSV to the import endpoint. dryRun asks the
 * backend to validate without writing (the report still lists every rejected line);
 * an apply is atomic, so a report with errors means nothing was written and the caller
 * should not refresh yet.
 */
export async function importPlacements(
  siteId: string,
  csv: string,
  dryRun: boolean,
): Promise<Schemas["MapImportReport"]> {
  return unwrap(
    await api.POST("/api/v1/maps/placements/import", {
      body: { site_id: siteId, csv, dry_run: dryRun },
    }),
  );
}
