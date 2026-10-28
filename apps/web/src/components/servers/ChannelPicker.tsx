import type { Schemas } from "@/api/client";
import { Checkbox, Pill } from "@/components/ui";
import { useT } from "@/i18n";

type Channel = Schemas["ProvisionNvrChannel"];

/**
 * ChannelPicker lists a recorder's channels with a checkbox each. A channel that already is a camera is
 * shown as imported and cannot be picked again.
 */
export function ChannelPicker({
  channels,
  selected,
  onChange,
}: {
  channels: Channel[];
  selected: ReadonlySet<string>;
  onChange: (next: Set<string>) => void;
}) {
  const t = useT();
  const pickable = channels.filter((c) => !c.imported);
  const allSelected = pickable.length > 0 && pickable.every((c) => selected.has(c.id));
  const toggle = (id: string, on: boolean) => {
    const next = new Set(selected);
    if (on) next.add(id);
    else next.delete(id);
    onChange(next);
  };
  return (
    <fieldset className="flex flex-col gap-1">
      <legend className="text-sm font-semibold">{t("servers.provisionChannelsTitle")}</legend>
      {pickable.length > 1 && (
        <Checkbox
          label={t("servers.provisionChannelsAll")}
          checked={allSelected}
          onChange={(on) => onChange(on ? new Set(pickable.map((c) => c.id)) : new Set())}
        />
      )}
      <ul className="flex flex-col">
        {channels.map((c) => (
          <li key={c.id}>
            <Checkbox
              checked={c.imported ? true : selected.has(c.id)}
              disabled={!!c.imported}
              onChange={(on) => toggle(c.id, on)}
              label={
                <span className="flex flex-wrap items-center gap-2">
                  <span>{c.name}</span>
                  {c.address && <span className="font-mono text-xs text-muted">{c.address}</span>}
                  {c.online === true && <Pill tone="ok" size="sm">{t("servers.provisionChannelOnline")}</Pill>}
                  {c.online === false && <Pill tone="bad" size="sm">{t("servers.provisionChannelOffline")}</Pill>}
                  {c.imported && <Pill tone="secondary" size="sm">{t("servers.provisionChannelImported")}</Pill>}
                </span>
              }
            />
          </li>
        ))}
      </ul>
    </fieldset>
  );
}
