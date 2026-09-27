import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import {
  cameraGroupsQuery, camerasQuery, grantsQuery, groupsQuery, meQuery, permissionCatalogQuery, serversQuery, sitesQuery, tenantsQuery, usersQuery,
} from "@/api/queries";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, Table, Th } from "@/components/ui";

type Scope = Schemas["ScopeType"];

const scopeDepth: Record<Scope, number> = { platform: 0, tenant: 1, site: 2, camera_group: 2, server: 3, camera: 4 };
const scopeName: Record<Scope, string> = {
  platform: "Plataforma",
  tenant: "Organización",
  site: "Sitio",
  server: "Servidor",
  camera_group: "Grupo de cámaras",
  camera: "Cámara",
};

// Bundles for the usual roles; each permission is granted separately (PRD §28).
const presets: Record<string, { label: string; perms: string[] }> = {
  operator: {
    label: "Operador (en vivo, eventos, grabaciones, patentes, exportar)",
    perms: [
      "cameras.view", "live.view", "events.view", "events.search", "events.review", "recordings.view", "recordings.seek",
      "snapshots.view", "snapshots.download", "lpr.view", "lpr.search", "exports.create", "exports.download",
      "views.create_private", "sites.view", "health.view",
    ],
  },
  viewer: { label: "Solo en vivo", perms: ["cameras.view", "live.view", "sites.view"] },
  investigator: {
    label: "Investigador (eventos, grabaciones y patentes, sin en vivo)",
    perms: ["cameras.view", "events.view", "events.search", "recordings.view", "recordings.seek", "snapshots.view", "lpr.view", "lpr.search", "exports.create", "exports.download", "sites.view"],
  },
  admin: { label: "Administrador (todo lo que se pueda otorgar en el alcance)", perms: ["*"] },
};

function grantable(def: Schemas["PermissionDefinition"], scope: Scope) {
  if (scope === "camera_group") return def.narrowest_scope === "camera";
  return scopeDepth[scope] <= scopeDepth[def.narrowest_scope];
}

/** Permissions assigns permission + scope + effect to users and groups (PRD §27-31). */
export function Permissions() {
  const search = useSearch({ from: "/app/permissions" });
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const users = useQuery(usersQuery);
  const groups = useQuery(groupsQuery);
  const catalog = useQuery(permissionCatalogQuery);
  const [subjectType, subjectId] = (search.subject ?? ":").split(":") as ["user" | "group" | "", string];
  const grants = useQuery(grantsQuery(subjectType || undefined, subjectId || undefined));
  const describe = useScopeNames();

  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/api/v1/grants/{grantId}", { params: { path: { grantId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["grants"] }),
  });

  const permDesc = new Map(catalog.data?.map((d) => [d.permission, d.description]));
  const subjectTenant =
    subjectType === "user" ? users.data?.find((u) => u.id === subjectId)?.tenant_id : groups.data?.find((g) => g.id === subjectId)?.tenant_id;

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title="Permisos" description="Un permiso vale en su alcance y todo lo que contiene. Una denegación siempre gana sobre un permiso." />
      <Field label="Usuario o grupo">
        <Select value={search.subject ?? ""} onChange={(e) => void navigate({ to: "/permissions", search: { subject: e.target.value || undefined } })}>
          <option value="">Elegí a quién</option>
          <optgroup label="Grupos">
            {groups.data?.map((g) => (
              <option key={g.id} value={`group:${g.id}`}>
                {g.name}
              </option>
            ))}
          </optgroup>
          <optgroup label="Usuarios">
            {users.data?.map((u) => (
              <option key={u.id} value={`user:${u.id}`}>
                {u.display_name} ({u.username})
              </option>
            ))}
          </optgroup>
        </Select>
      </Field>
      {subjectId && (
        <>
          <GrantForm
            subjectType={subjectType as "user" | "group"}
            subjectId={subjectId}
            tenantId={subjectTenant ?? me.data?.tenant_id ?? null}
            catalog={catalog.data ?? []}
          />
          <ErrorNote error={grants.error ?? remove.error} />
          {grants.data?.length === 0 && <Empty>Sin permisos directos.</Empty>}
          {!!grants.data?.length && (
            <Table label="Permisos otorgados">
              <thead>
                <tr>
                  <Th>Permiso</Th>
                  <Th>Efecto</Th>
                  <Th>Alcance</Th>
                  <Th />
                </tr>
              </thead>
              <tbody>
                {grants.data.map((g) => (
                  <tr key={g.id} className="border-t border-line">
                    <td>
                      <div className="font-mono text-xs">{g.permission}</div>
                      <div className="text-xs text-muted">{permDesc.get(g.permission)}</div>
                    </td>
                    <td className={g.effect === "deny" ? "text-bad" : "text-ok"}>{g.effect === "deny" ? "Denegar" : "Permitir"}</td>
                    <td className="text-sm">
                      {scopeName[g.scope_type]}
                      {g.scope_id && <span className="text-muted">: {describe(g.scope_type, g.scope_id)}</span>}
                    </td>
                    <td className="text-right">
                      <Button onClick={() => remove.mutate(g.id)} aria-label="Revocar" title="Revocar">
                        <Trash2 className="size-3.5" aria-hidden />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </>
      )}
    </div>
  );
}

function useScopeNames() {
  const tenants = useQuery(tenantsQuery);
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const cams = useQuery(camerasQuery({}));
  const cgroups = useQuery(cameraGroupsQuery);
  return (t: Scope, id: string) => {
    const lists: Partial<Record<Scope, { id: string; name?: string; display_name?: string }[] | undefined>> = {
      tenant: tenants.data,
      site: sites.data,
      server: servers.data,
      camera: cams.data,
      camera_group: cgroups.data,
    };
    const item = lists[t]?.find((x) => x.id === id);
    return item?.display_name ?? item?.name ?? id.slice(0, 8);
  };
}

function GrantForm({
  subjectType,
  subjectId,
  tenantId,
  catalog,
}: {
  subjectType: "user" | "group";
  subjectId: string;
  tenantId: string | null;
  catalog: Schemas["PermissionDefinition"][];
}) {
  const qc = useQueryClient();
  const tenants = useQuery(tenantsQuery);
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const cams = useQuery(camerasQuery({}));
  const cgroups = useQuery(cameraGroupsQuery);
  const [what, setWhat] = useState("preset:operator");
  const [effect, setEffect] = useState<"allow" | "deny">("allow");
  const [scope, setScope] = useState<Scope>("site");
  const [target, setTarget] = useState("");
  const [result, setResult] = useState("");

  const targets: { id: string; label: string }[] =
    scope === "tenant"
      ? (tenants.data ?? []).filter((t) => !tenantId || t.id === tenantId).map((t) => ({ id: t.id, label: t.name }))
      : scope === "site"
        ? (sites.data ?? []).filter((s) => !tenantId || s.tenant_id === tenantId).map((s) => ({ id: s.id, label: s.name }))
        : scope === "server"
          ? (servers.data ?? []).filter((s) => !tenantId || s.tenant_id === tenantId).map((s) => ({ id: s.id, label: s.name }))
          : scope === "camera"
            ? (cams.data ?? []).filter((c) => !tenantId || c.tenant_id === tenantId).map((c) => ({ id: c.id, label: c.display_name }))
            : scope === "camera_group"
              ? (cgroups.data ?? []).filter((g) => !tenantId || g.tenant_id === tenantId).map((g) => ({ id: g.id, label: g.name }))
              : [];

  const grant = useMutation({
    mutationFn: async () => {
      let perms: string[];
      if (what.startsWith("preset:")) {
        const p = presets[what.slice(7)];
        if (!p) throw new Error("Unknown permission preset");
        perms = p.perms[0] === "*" ? catalog.map((d) => d.permission) : p.perms;
      } else perms = [what];
      const defs = new Map(catalog.map((d) => [d.permission, d]));
      const applicable = perms.filter((p) => defs.has(p) && grantable(defs.get(p)!, scope));
      let ok = 0;
      const failed: string[] = [];
      for (const permission of applicable) {
        const { response, error } = await api.POST("/api/v1/grants", {
          body: { subject_type: subjectType, subject_id: subjectId, permission, effect, scope_type: scope, scope_id: scope === "platform" ? undefined : target },
        });
        if (response.ok || response.status === 409) ok++;
        else failed.push(`${permission}: ${(error as { message?: string } | undefined)?.message ?? response.status}`);
      }
      const skipped = perms.length - applicable.length;
      return { ok, failed, skipped };
    },
    onSuccess: (r) => {
      setResult(
        `${r.ok} permisos aplicados` +
          (r.skipped ? `, ${r.skipped} no aplican a este alcance` : "") +
          (r.failed.length ? `. Fallaron: ${r.failed.join("; ")}` : "."),
      );
      void qc.invalidateQueries({ queryKey: ["grants"] });
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });

  return (
    <form
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        setResult("");
        grant.mutate();
      }}
      className="grid gap-3 rounded border border-line bg-surface p-3 sm:grid-cols-2 lg:grid-cols-4"
    >
      <Field label="Qué">
        <Select value={what} onChange={(e) => setWhat(e.target.value)}>
          <optgroup label="Perfiles">
            {Object.entries(presets).map(([k, p]) => (
              <option key={k} value={`preset:${k}`}>
                {p.label}
              </option>
            ))}
          </optgroup>
          <optgroup label="Permiso individual">
            {catalog.map((d) => (
              <option key={d.permission} value={d.permission}>
                {d.description} ({d.permission})
              </option>
            ))}
          </optgroup>
        </Select>
      </Field>
      <Field label="Efecto">
        <Select value={effect} onChange={(e) => setEffect(e.target.value as "allow" | "deny")}>
          <option value="allow">Permitir</option>
          <option value="deny">Denegar</option>
        </Select>
      </Field>
      <Field label="Alcance">
        <Select
          value={scope}
          onChange={(e) => {
            setScope(e.target.value as Scope);
            setTarget("");
          }}
        >
          {(["tenant", "site", "server", "camera_group", "camera"] as Scope[]).map((s) => (
            <option key={s} value={s}>
              {scopeName[s]}
            </option>
          ))}
          {tenantId === null && <option value="platform">Plataforma</option>}
        </Select>
      </Field>
      {scope !== "platform" ? (
        <Field label={scopeName[scope]}>
          <Select required value={target} onChange={(e) => setTarget(e.target.value)}>
            <option value="">Elegí</option>
            {targets.map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </Select>
        </Field>
      ) : (
        <div />
      )}
      <div className="flex flex-wrap items-center gap-3 sm:col-span-2 lg:col-span-4">
        <Button type="submit" variant="primary" disabled={grant.isPending}>
          {grant.isPending ? "Aplicando…" : "Otorgar"}
        </Button>
        {result && <span className="text-sm text-muted">{result}</span>}
        <ErrorNote error={grant.error} />
      </div>
    </form>
  );
}
