import { useQuery } from "@tanstack/react-query";
import { serversQuery, sitesQuery } from "@/api/queries";
import { Field, Select } from "@/components/ui";
import { useT } from "@/i18n";

export type Scope = { site: string; server: string };
export const emptyScope: Scope = { site: "", server: "" };

/** ScopeFilters narrows a faces list to one site and/or one server, like the LPR search does. */
export function ScopeFilters({ value, onChange }: { value: Scope; onChange: (next: Scope) => void }) {
  const t = useT();
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const serverOptions = (servers.data ?? []).filter((s) => !value.site || s.site_id === value.site);
  return (
    <div className="grid gap-3 rounded-m3-xl bg-surface-1 p-4 sm:grid-cols-2">
      <Field label={t("common.site")}>
        <Select value={value.site} onChange={(e) => onChange({ site: e.target.value, server: "" })}>
          <option value="">{t("common.all")}</option>
          {sites.data?.map((s) => (
            <option key={s.id} value={s.id}>{s.name}</option>
          ))}
        </Select>
      </Field>
      <Field label={t("common.server")}>
        <Select value={value.server} onChange={(e) => onChange({ ...value, server: e.target.value })}>
          <option value="">{t("common.all")}</option>
          {serverOptions.map((s) => (
            <option key={s.id} value={s.id}>{s.name}</option>
          ))}
        </Select>
      </Field>
    </div>
  );
}
