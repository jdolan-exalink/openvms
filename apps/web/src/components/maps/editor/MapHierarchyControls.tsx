import { useState } from "react";
import { createPortal } from "react-dom";
import { Map } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { Modal } from "@/components/Modal";
import { Button, Field, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { convertPlan } from "@/lib/maps/planConversion";
import { uploadFloorPlan } from "@/lib/maps/plans";
import { PlanUpload } from "./PlanUpload";
import { RoomPlannerModal } from "./RoomPlannerModal";

type Floor = Schemas["MapFloor"];
type Kind = "geo" | "image" | "draw";
type View =
  | { kind: "list" }
  | { kind: "create" }
  | { kind: "edit"; floor: Floor };

interface Props {
  site: Schemas["MapSiteDetails"];
  activeFloor?: Floor;
  disabled: boolean;
  onSaved: () => void;
  onCreated: (id: string) => void;
  onRemoved: () => void;
  onOpenGeographic?: () => void;
  /** A launcher in the editor sidebar. The catalog itself always opens in a dialog. */
  embedded?: boolean;
}

/** Catalog of the site's geographic map and image plans. Ordinals stay internal. */
export function MapHierarchyControls({ site, activeFloor, disabled, onSaved, onCreated, onRemoved, onOpenGeographic, embedded = false }: Props) {
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<View>({ kind: "list" });
  const [name, setName] = useState("");
  const [kind, setKind] = useState<Kind | "">("");
  const [planFile, setPlanFile] = useState<File>();
  const [plannerFloor, setPlannerFloor] = useState<Floor>();
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const floors = site.buildings.flatMap((building) => building.floors.map((floor) => ({ building, floor })));
  const editing = view.kind === "edit" ? floors.find((item) => item.floor.id === view.floor.id)?.floor ?? view.floor : undefined;

  function openDialog() {
    setView({ kind: "list" });
    setError(undefined);
    setConfirming(false);
    setOpen(true);
  }

  function close() {
    setOpen(false);
    setView({ kind: "list" });
    setError(undefined);
    setConfirming(false);
  }

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setError(undefined);
    try {
      await action();
      onSaved();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      onSaved();
    } finally {
      setBusy(false);
    }
  }

  function nextOrdinal() {
    const ordinals = floors.map((item) => item.floor.ordinal);
    return ordinals.length ? Math.max(...ordinals) + 1 : 0;
  }

  async function ensureBuilding() {
    const existing = site.buildings[0];
    if (existing) return existing.id;
    const created = unwrap(await api.POST("/api/v1/maps/sites/{siteId}/buildings", {
      params: { path: { siteId: site.id } },
      body: { name: "Planos" },
    }));
    return created.id;
  }

  async function createImage() {
    const buildingId = await ensureBuilding();
    const created = unwrap(await api.POST("/api/v1/maps/sites/{siteId}/buildings/{buildingId}/floors", {
      params: { path: { siteId: site.id, buildingId } },
      body: { name: name.trim(), ordinal: nextOrdinal() },
    }));
    onCreated(created.id);
    setView({ kind: "edit", floor: created });
    setName(created.name);
    setConfirming(false);
    if (planFile) {
      const converted = await convertPlan(planFile, { page: 1 });
      if (created.revision == null) throw new Error("La revisión del mapa no está disponible.");
      await uploadFloorPlan(site.id, created.id, created.revision, converted.blob);
      setPlanFile(undefined);
    }
  }

  async function createFloorAndOpenDesigner() {
    const buildingId = await ensureBuilding();
    const created = unwrap(await api.POST("/api/v1/maps/sites/{siteId}/buildings/{buildingId}/floors", {
      params: { path: { siteId: site.id, buildingId } },
      body: { name: name.trim(), ordinal: nextOrdinal() },
    }));
    setPlannerFloor(created);
  }

  async function rename(floor: Floor) {
    if (floor.revision == null) throw new Error("La revisión del mapa no está disponible.");
    unwrap(await api.PATCH("/api/v1/maps/sites/{siteId}/floors/{floorId}", {
      params: { path: { siteId: site.id, floorId: floor.id }, header: { "If-Match": String(floor.revision) } },
      body: { name: name.trim(), ordinal: floor.ordinal },
    }));
    setView({ kind: "list" });
  }

  async function remove(floor: Floor) {
    if (floor.revision == null) throw new Error("La revisión del mapa no está disponible.");
    unwrap(await api.DELETE("/api/v1/maps/sites/{siteId}/floors/{floorId}", {
      params: { path: { siteId: site.id, floorId: floor.id }, header: { "If-Match": String(floor.revision) } },
    }));
    if (activeFloor?.id === floor.id) onRemoved();
    setView({ kind: "list" });
    setConfirming(false);
  }

  const panel = (
    <div className="space-y-4">
      {error && <p role="alert" className="rounded-m3-lg bg-bad/10 px-3 py-2 text-sm text-bad">{error}</p>}
      {view.kind === "list" && (
        <div className="space-y-3">
          <ul className="space-y-2">
            <li className="flex flex-wrap items-center gap-3 rounded-m3-lg bg-surface-2 px-4 py-3">
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-bold">Mapa geográfico</span>
                <span className="text-xs text-on-surface-variant">Mapa real · usa coordenadas</span>
              </span>
              <Button variant="tonal" onClick={() => { onOpenGeographic?.(); close(); }}>Abrir</Button>
            </li>
            {floors.map(({ floor }) => (
              <li key={floor.id} className="flex flex-wrap items-center gap-3 rounded-m3-lg bg-surface-2 px-4 py-3">
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-bold">{floor.name}</span>
                  <span className="text-xs text-on-surface-variant">Fondo de imagen</span>
                </span>
                <Button variant="tonal" aria-label={`Editar ${floor.name}`} onClick={() => { setName(floor.name); setConfirming(false); setError(undefined); setView({ kind: "edit", floor }); }}>Editar</Button>
                <Button variant="outlined" className="border-bad text-bad" aria-label={`Borrar ${floor.name}`} onClick={() => { setName(floor.name); setConfirming(true); setError(undefined); setView({ kind: "edit", floor }); }}>Borrar</Button>
              </li>
            ))}
          </ul>
          <Button variant="primary" onClick={() => { setName(""); setKind(""); setPlanFile(undefined); setError(undefined); setView({ kind: "create" }); }}>Nuevo mapa</Button>
        </div>
      )}
      {view.kind === "create" && (
        <form className="space-y-4" onSubmit={(event) => {
          event.preventDefault();
          if (kind === "geo") { onOpenGeographic?.(); close(); return; }
          if (kind === "draw") { void run(createFloorAndOpenDesigner); return; }
          void run(createImage);
        }}>
          <fieldset className="space-y-2">
            <legend className="text-sm font-bold">Tipo de mapa</legend>
            <TypeOption selected={kind === "geo"} value="geo" title="Plano real (Geográfico)" detail="El mapa geoespacial y satelital del sitio, con coordenadas GPS reales." onSelect={() => setKind("geo")} />
            <TypeOption selected={kind === "image"} value="image" title="Subir imagen o plano CAD" detail="Un archivo de plano existente (PNG, SVG o PDF)." onSelect={() => setKind("image")} />
            <TypeOption selected={kind === "draw"} value="draw" title="Diseñar plano" detail="Crear un plano personalizado dibujando paredes, puertas, ventanas y ambientes." onSelect={() => setKind("draw")} />
          </fieldset>
          {(kind === "image" || kind === "draw") && (
            <Field label="Nombre del mapa">
              <TextInput aria-label="Nombre del mapa" value={name} placeholder="Ej. Planta Baja, Depósito Central..." onChange={(event) => setName(event.target.value)} />
            </Field>
          )}
          {kind === "image" && (
            <>
              <label className="block text-sm">
                Imagen de fondo
                <input aria-label="Imagen de fondo" type="file" accept="image/png,image/svg+xml,application/pdf,.png,.svg,.pdf" className="mt-1 block w-full rounded-m3-md bg-surface-2 p-2 text-sm file:mr-3 file:h-9 file:rounded-full file:border-0 file:bg-secondary-container file:px-4 file:text-sm file:font-bold file:text-on-secondary-container" onChange={(event) => setPlanFile(event.target.files?.[0])} />
              </label>
              {planFile && <p className="text-sm text-on-surface-variant">{planFile.name}</p>}
              <p className="text-sm text-on-surface-variant">PNG, SVG o PDF. Al crearlo se guarda el fondo y después podés girarlo para acomodarlo.</p>
            </>
          )}
          {kind === "draw" && (
            <p className="text-sm text-on-surface-variant">
              Al hacer clic en «Crear y diseñar plano», se abrirá el editor interactivo para dibujar paredes, aberturas y ambientes a escala.
            </p>
          )}
          {kind === "geo" && <p className="text-sm text-on-surface-variant">Este sitio tiene un mapa real. Abrirlo muestra el mapa geográfico.</p>}
          <div className="flex gap-2">
            <Button variant="primary" type="submit" disabled={busy || !kind || (kind !== "geo" && !name.trim())}>
              {kind === "geo" ? "Usar mapa real" : kind === "draw" ? "Crear y diseñar plano" : "Crear mapa"}
            </Button>
            <Button variant="outlined" onClick={() => setView({ kind: "list" })}>Volver</Button>
          </div>
        </form>
      )}
      {view.kind === "edit" && editing && (
        <div className="space-y-4">
          <Field label="Nombre del mapa">
            <TextInput aria-label="Nombre del mapa" value={name} onChange={(event) => setName(event.target.value)} />
          </Field>
          <Button variant="primary" disabled={busy || !name.trim() || editing.revision == null} onClick={() => void run(() => rename(editing))}>Guardar</Button>
          {editing.revision != null && <PlanUpload siteId={site.id} floorId={editing.id} revision={editing.revision} onSaved={onSaved} />}
          {confirming ? (
            <div className="space-y-2 rounded-m3-lg bg-bad/10 p-4 text-sm">
              <p>¿Borrar «{editing.name}»? Solo se puede si no tiene cámaras ni zonas.</p>
              <div className="flex gap-2">
                <Button variant="danger" disabled={busy} onClick={() => void run(() => remove(editing))}>Eliminar mapa</Button>
                <Button variant="outlined" onClick={() => setConfirming(false)}>Volver</Button>
              </div>
            </div>
          ) : (
            <Button variant="outlined" className="border-bad text-bad" onClick={() => setConfirming(true)}>Borrar</Button>
          )}
          <Button variant="text" onClick={() => { setConfirming(false); setView({ kind: "list" }); }}>Volver a la lista</Button>
        </div>
      )}
    </div>
  );

  const dialog = open ? (
    <>
      {createPortal(<Modal title="Mapas" onClose={close}>{panel}</Modal>, document.body)}
      {plannerFloor && (
        <RoomPlannerModal
          mapName={plannerFloor.name}
          onSave={async (file, state) => {
            setBusy(true);
            setError(undefined);
            try {
              const converted = await convertPlan(file, { page: 1 });
              if (plannerFloor.revision == null) throw new Error("La revisión del mapa no está disponible.");
              await uploadFloorPlan(site.id, plannerFloor.id, plannerFloor.revision, converted.blob);
              try {
                localStorage.setItem(`openvms_floor_plan_${plannerFloor.id}`, JSON.stringify(state));
              } catch (_) {}
              onSaved();
              onCreated(plannerFloor.id);
              setPlannerFloor(undefined);
              close();
            } catch (cause) {
              setError(cause instanceof Error ? cause.message : String(cause));
              throw cause;
            } finally {
              setBusy(false);
            }
          }}
          onClose={() => {
            if (plannerFloor) {
              onCreated(plannerFloor.id);
              setView({ kind: "edit", floor: plannerFloor });
            }
            setPlannerFloor(undefined);
          }}
        />
      )}
    </>
  ) : null;

  if (embedded) {
    return (
      <>
        <Button variant="tonal" className="w-full" disabled={disabled} onClick={openDialog}>
          <Map className="size-4" aria-hidden />
          Editor de mapas
        </Button>
        {dialog}
      </>
    );
  }

  return (
    <>
      <Button variant="tonal" disabled={disabled} onClick={openDialog}>Mapas</Button>
      {dialog}
    </>
  );
}

function TypeOption({ selected, value, title, detail, onSelect }: { selected: boolean; value: Kind; title: string; detail: string; onSelect: () => void }) {
  return (
    <label className={cn("flex min-h-11 items-start gap-3 rounded-m3-lg px-4 py-3", selected ? "bg-primary-container text-on-primary-container" : "bg-surface-2")}>
      <input className="mt-1 size-4 accent-primary" type="radio" name="tipo-de-mapa" value={value} checked={selected} onChange={onSelect} />
      <span>
        <span className="block text-sm font-bold">{title}</span>
        <span className="text-xs opacity-80">{detail}</span>
      </span>
    </label>
  );
}
