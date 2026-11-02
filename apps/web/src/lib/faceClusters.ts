import { api, unwrap } from "@/api/client";

export type ClusterTarget = { personId: string } | { newName: string };

/** The sightings that were assigned before one failed; the rest stay pending and can be retried. */
export class PartialAssignError extends Error {
  constructor(
    readonly done: number,
    readonly total: number,
    readonly personId: string | null,
    cause: unknown,
  ) {
    super(cause instanceof Error ? cause.message : String(cause));
  }
}

/**
 * assignCluster gives every sighting of a group to one person. The first call creates the person when a
 * name was typed, and the rest reuse it, so a failure half way never leaves two people with the same name.
 */
export async function assignCluster(sightingIds: string[], target: ClusterTarget): Promise<{ personId: string; name: string; count: number }> {
  let personId = "personId" in target ? target.personId : null;
  let name = "newName" in target ? target.newName.trim() : "";
  let done = 0;
  for (const sightingId of sightingIds) {
    try {
      const body = personId ? { person_id: personId } : { new_person_display_name: name };
      const res = unwrap(await api.POST("/api/v1/faces/review-inbox/{sightingId}/assign", { params: { path: { sightingId } }, body }));
      personId = res.person.id;
      name = res.person.display_name;
      done++;
    } catch (err) {
      throw new PartialAssignError(done, sightingIds.length, personId, err);
    }
  }
  return { personId: personId ?? "", name, count: done };
}

export async function dismissCluster(sightingIds: string[]): Promise<void> {
  for (const sightingId of sightingIds) {
    unwrap(await api.DELETE("/api/v1/faces/review-inbox/{sightingId}", { params: { path: { sightingId } } }));
  }
}
