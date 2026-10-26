import { createContext, type ReactNode, useContext } from "react";

type Ctx = { openYaml: (section: string) => void };
const YamlEntryCtx = createContext<Ctx | null>(null);

/** Lets tabs open the YAML editor of a Frigate section in Avanzado without knowing the editor. */
export function YamlEntryProvider({ openYaml, children }: { openYaml?: (section: string) => void; children: ReactNode }) {
  return <YamlEntryCtx.Provider value={openYaml ? { openYaml } : null}>{children}</YamlEntryCtx.Provider>;
}

/**
 * Small contextual link ("Editar YAML de detección"). Renders nothing outside the provider, which
 * is also how a user without cameras.config.yaml gets no entry point.
 */
export function EditYamlLink({ section, label }: { section: string; label: string }) {
  const ctx = useContext(YamlEntryCtx);
  if (!ctx) return null;
  return (
    <button type="button" className="self-start text-xs font-semibold text-primary underline" onClick={() => ctx.openYaml(section)}>
      {label}
    </button>
  );
}
