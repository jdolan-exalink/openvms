import type { Schemas } from "@/api/client";
import type { SiteNode } from "@/lib/cameraTree";
import { CameraListHeader, CameraRow } from "./CameraRow";
import { ServerRow } from "./ServerRow";

type RowProps = Omit<Parameters<typeof CameraRow>[0], "camera" | "recorder" | "picked" | "onPick" | "analytics">;

/**
 * The camera list. With every server it is a Site > Server > Camera tree (collapsible per server); with a
 * server selected (`flat`) it is that server's cameras under its own row. It is a container so rows reflow by
 * the width of the list, not of the window.
 */
export function CameraTree({
  tree,
  flat,
  collapsed,
  forceOpen,
  onToggle,
  picked,
  onPick,
  analyticsOf,
  ...row
}: {
  tree: SiteNode<Schemas["Camera"], Schemas["Server"]>[];
  flat: boolean;
  collapsed: ReadonlySet<string>;
  /** A search opens every group so a match is never hidden inside a collapsed server. */
  forceOpen: boolean;
  onToggle: (serverId: string) => void;
  picked: readonly string[];
  onPick: (cameraId: string) => void;
  analyticsOf: (cameraId: string) => { enabled: boolean; effective?: boolean } | undefined;
} & RowProps) {
  return (
    <div aria-label="Cámaras" className="@container min-w-0 flex-1 overflow-hidden rounded-m3-xl bg-surface-1">
      <CameraListHeader selectable={row.selectable} />
      {tree.map((siteNode) => (
        <section key={siteNode.site.id} aria-label={`Sitio ${siteNode.site.name}`}>
          {!flat && <h2 className="px-3 pt-3 pb-1 text-xs font-bold tracking-wide text-muted uppercase">{siteNode.site.name}</h2>}
          {siteNode.servers.map(({ server, cameras, online, agent }) => {
            const open = flat || forceOpen || !collapsed.has(server.id);
            return (
              <div key={server.id} role="group" aria-label={server.name}>
                <ServerRow
                  name={server.name}
                  siteId={server.site_id ?? siteNode.site.id}
                  kind={server.kind}
                  agent={agent}
                  total={cameras.length}
                  online={online}
                  open={flat ? undefined : open}
                  onToggle={flat ? undefined : () => onToggle(server.id)}
                />
                {open &&
                  cameras.map((c) => (
                    <CameraRow
                      key={c.id}
                      camera={c}
                      recorder={server.kind === "provision_nvr"}
                      picked={picked.includes(c.id)}
                      onPick={() => onPick(c.id)}
                      analytics={analyticsOf(c.id)}
                      {...row}
                    />
                  ))}
              </div>
            );
          })}
        </section>
      ))}
    </div>
  );
}
