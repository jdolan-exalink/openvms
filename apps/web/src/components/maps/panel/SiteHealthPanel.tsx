import type { CameraEntity } from "@/lib/maps/types";
import { groupSiteHealth } from "@/lib/maps/incidentPolicy";

export function SiteHealthPanel({ cameras, siteId, onSite, onCamera }: {
  cameras: CameraEntity[]; siteId?: string;
  onSite: (id: string) => void; onCamera: (id: string) => void;
}) {
  const groups = groupSiteHealth(cameras);
  return <section aria-label="Site health" className="rounded border border-border bg-card p-3">
    <h2>Site health</h2>
    {siteId && <button onClick={() => onSite(siteId)}>View site</button>}
    {!groups.length && <p>No availability incidents</p>}
    <ul>{groups.map(group => <li key={`${group.kind}:${group.id}`}>
      <h3>{group.name}</h3><p>{group.cameraIds.length} cameras affected</p>
      {group.cameraIds.map(id => <button key={id} onClick={() => onCamera(id)}>
        View {cameras.find(camera => camera.id === id)?.name ?? id}
      </button>)}
    </li>)}</ul>
  </section>;
}
