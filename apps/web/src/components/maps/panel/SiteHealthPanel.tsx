import { Button } from "@/components/ui";
import type { CameraEntity } from "@/lib/maps/types";
import { groupSiteHealth } from "@/lib/maps/incidentPolicy";

export function SiteHealthPanel({ cameras, siteId, onSite, onCamera }: {
  cameras: CameraEntity[]; siteId?: string;
  onSite: (id: string) => void; onCamera: (id: string) => void;
}) {
  const groups = groupSiteHealth(cameras);
  return <section aria-label="Site health" className="flex flex-col gap-3 rounded-m3-xl bg-surface-1 p-4">
    <h2 className="text-lg font-bold">Site health</h2>
    {siteId && <Button variant="tonal" size="sm" onClick={() => onSite(siteId)}>View site</Button>}
    {!groups.length && <p className="text-sm text-on-surface-variant">No availability incidents</p>}
    <ul className="space-y-2">{groups.map(group => <li key={`${group.kind}:${group.id}`} className="flex flex-col items-start gap-2 rounded-m3-lg bg-surface-2 p-3">
      <h3 className="font-bold">{group.name}</h3><p className="font-mono text-xs text-on-surface-variant">{group.cameraIds.length} cameras affected</p>
      {group.cameraIds.map(id => <Button key={id} variant="text" size="sm" onClick={() => onCamera(id)}>
        View {cameras.find(camera => camera.id === id)?.name ?? id}
      </Button>)}
    </li>)}</ul>
  </section>;
}
