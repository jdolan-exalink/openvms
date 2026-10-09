import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Trash2 } from "lucide-react";
import { Fragment, type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import {
  cameraGroupsQuery, camerasQuery, grantsQuery, groupsQuery, meQuery, permissionCatalogQuery, serversQuery, sitesQuery, tenantsQuery, usersQuery,
} from "@/api/queries";
import { Button, Empty, IconButton, ErrorNote, Field, PageHeader, Select, Summary, Table, TextInput, Th } from "@/components/ui";

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

function summarizeGrants(n: number) {
  const noun = n === 1 ? "permiso directo" : "permisos directos";
  return `${n} ${noun}`;
}

/** Permissions assigns permission + scope + effect to users and groups (PRD §27-31). */
export function Permissions() {
  const t = useT();
  const search = useSearch({ strict: false }) as { subject?: string };
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const users = useQuery(usersQuery);
  const groups = useQuery(groupsQuery);
  const catalog = useQuery(permissionCatalogQuery);
  const [subjectQuery, setSubjectQuery] = useState("");
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

  const needle = subjectQuery.trim().toLowerCase();
  const filteredGroups = groups.data?.filter((g) => !needle || g.name.toLowerCase().includes(needle)) ?? [];
  const filteredUsers =
    users.data?.filter(
      (u) => !needle || [u.username, u.display_name, u.email ?? ""].some((v) => v.toLowerCase().includes(needle)),
    ) ?? [];

  const grouped = groupGrants(grants.data ?? [], describe);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title={t("nav.permissions")} description={t("settings.permissionRule")} />
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
        <div className="sm:w-72">
          <Field label="Buscar sujeto">
            <TextInput
              aria-label="Buscar sujeto"
              placeholder="Filtrar por nombre o usuario"
              value={subjectQuery}
              onChange={(e) => setSubjectQuery(e.target.value)}
            />
          </Field>
        </div>
        <div className="flex-1">
          <Field label="Usuario o grupo">
            <Select
              aria-label="Usuario o grupo"
              value={search.subject ?? ""}
              onChange={(e) => {
                const val = e.target.value;
                void navigate({
                  to: ".",
                  search: (prev: Record<string, unknown>) => {
                    const next = { ...prev };
                    if (val) next.subject = val;
                    else delete next.subject;
                    return next;
                  },
                });
              }}
            >
              <option value="">Elegí a quién</option>
              {filteredGroups.length > 0 && (
                <optgroup label="Grupos">
                  {filteredGroups.map((g) => (
                    <option key={g.id} value={`group:${g.id}`}>
                      {g.name}
                    </option>
                  ))}
                </optgroup>
              )}
              {filteredUsers.length > 0 && (
                <optgroup label="Usuarios">
                  {filteredUsers.map((u) => (
                    <option key={u.id} value={`user:${u.id}`}>
                      {u.display_name} ({u.username})
                    </option>
                  ))}
                </optgroup>
              )}
            </Select>
          </Field>
        </div>
      </div>
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
            <div className="flex flex-col gap-3">
              <div className="flex items-center justify-between">
                <span className="text-lg font-bold">Permisos directos asignados</span>
                <Summary>{summarizeGrants(grants.data.length)}</Summary>
              </div>
              <Table label="Permisos otorgados">
                <thead>
                  <tr>
                    <Th>Alcance / Permiso</Th>
                    <Th>Efecto</Th>
                    <Th>Descripción</Th>
                    <Th />
                  </tr>
                </thead>
                <tbody>
                  {grouped.map((group) => (
                    <Fragment key={group.label}>
                      <tr className="bg-surface-2 text-xs font-bold text-on-surface">
                        <td colSpan={4} className="px-3 py-2.5">
                          <span>{group.label}</span>
                          <span className="ml-2 font-normal text-muted">({group.grants.length})</span>
                        </td>
                      </tr>
                      {group.grants.map((g) => (
                        <tr key={g.id} className="border-t border-outline-variant">
                          <td className="pl-6">
                            <div className="font-mono text-xs font-semibold">{g.permission}</div>
                          </td>
                          <td>
                            <span
                              className={
                                g.effect === "deny"
                                  ? "inline-flex h-6 items-center rounded-full bg-bad/15 px-2.5 text-xs font-medium text-bad"
                                  : "inline-flex h-6 items-center rounded-full bg-ok/15 px-2.5 text-xs font-medium text-ok"
                              }
                            >
                              {g.effect === "deny" ? "Denegar" : "Permitir"}
                            </span>
                          </td>
                          <td className="text-xs text-muted">{permDesc.get(g.permission) ?? "—"}</td>
                          <td className="text-right">
                            <IconButton
                              icon={Trash2}
                              onClick={() => remove.mutate(g.id)}
                              aria-label={`Revocar ${g.permission}`}
                              title={`Revocar ${g.permission}`}
                              className="text-on-surface-variant hover:bg-bad/15 hover:text-bad"
                            />
                          </td>
                        </tr>
                      ))}
                    </Fragment>
                  ))}
                </tbody>
              </Table>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function groupGrants(grants: Schemas["Grant"][], describe: (t: Scope, id: string) => string) {
  const groups = new Map<string, { label: string; scope: Scope; grants: Schemas["Grant"][] }>();
  for (const g of grants) {
    const scopeLabel =
      g.scope_type === "platform"
        ? "Plataforma"
        : `${scopeName[g.scope_type]}: ${g.scope_id ? describe(g.scope_type, g.scope_id) : ""}`;
    const key = `${g.scope_type}:${g.scope_id ?? ""}`;
    if (!groups.has(key)) {
      groups.set(key, { label: scopeLabel, scope: g.scope_type, grants: [] });
    }
    groups.get(key)!.grants.push(g);
  }
  return Array.from(groups.values());
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
      className="grid gap-3 rounded-m3-xl bg-surface-1 p-5 sm:grid-cols-2 lg:grid-cols-4"
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
        <Button type="submit" variant="filled" disabled={grant.isPending}>
          {grant.isPending ? "Aplicando…" : "Otorgar"}
        </Button>
        {result && <span className="text-sm text-muted">{result}</span>}
        <ErrorNote error={grant.error} />
      </div>
    </form>
  );
}
