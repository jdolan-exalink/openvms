# Object catalog

`internal/camconfig/catalog` is the normalized list of detectable objects. The UI shows this catalog, never an engine's raw label list (Frigate's model exposes 80+ labels).

## Model

- `Category` (`id`, Spanish `label`, ordered `Objects`). A category id is also a valid selection meaning "every member" (the "Activar todos los vehículos" toggle).
- `Object` (`id`, `label`, `Engine`, `RequiresModel`). `Engine` maps an engine kind (`frigate`, later `provision`) to the labels that engine uses. A missing mapping means the engine cannot report that object; the catalog never invents precision (for example Frigate has no pickup class, so `vehicle.pickup` has no Frigate labels).
- `RequiresModel` marks objects present only when the engine model supports them (PPE).

Ids are stable machine names (`person`, `vehicle.car`, `package.box`, `safety.helmet`). The person category is `people` so it does not clash with the `person` object. Order is part of the contract: the UI renders it as is.

## Functions

| Function | Purpose |
|---|---|
| `Default()` | Built-in catalog. |
| `ExpandToEngine(engine, ids)` | Object or category ids to sorted, deduplicated engine labels. |
| `ExpandForModel(engine, ids, available)` | Same, restricted to labels the model supports; also returns requested ids that cannot be honored (unknown ids, explicit objects with no usable label). Members reached only through a category are skipped silently. |
| `CollapseFromEngine(engine, labels)` | Labels to `Selection{Objects, Categories, Raw}`. A category counts as selected only when every member the engine can report is selected. Unknown labels go to `Raw`, so a round trip loses nothing. |
| `Classify(engine, label)` | Normalizes one event label to object and category. The caller keeps the original label for advanced search and audit. |

Frigate labels not meant for surveillance (COCO household items) live in `other.misc`; `license_plate` and `face` are `other.*` objects because they are analytics modules.

## Provision mapping

Provision ISR events carry a plate and, at most, a coarse vehicle class (`internal/agent/provisionlpr`: `plate`, `vehicle_type` free text, color, brand). Only that is mapped:

| Provision label | Object |
|---|---|
| `vehicle` | `vehicle.generic` ("Vehículo (sin tipo)", last of the Vehículos category) |
| `license_plate` | `other.license_plate` |

Car, truck, bus and the rest have no Provision label: they stay unsupported for that engine, and the Frigate view of `vehicle.generic` is empty. Selecting "Vehículos" on a Provision camera therefore means "vehicle, type unknown". `TestProvisionLabelsBelongToOneObject` pins that nothing else gets mapped without evidence.

## Adding an engine mapping

Add an entry to the object's `Engine` map in `default.go` (`"provision": {"vehicle"}`). A label may belong to only one object per engine; `New` rejects duplicates and `TestEveryLabelBelongsToOneObject` guards the default data. An engine that reports only a coarse class (Provision: `vehicle`) maps it to the coarsest object that is honest, and leaves finer objects unmapped.

## Adding a category or object

Append to `Default()` in the position the UI should show it, with a unique id (`<category>.<name>`), then run `go test ./internal/camconfig/catalog/...`.

## API and selection semantics

- `GET /api/v1/camera-config/object-catalog?engine=frigate[&server_id=...]` (any authenticated actor) returns the ordered categories and objects with `id`, Spanish `label`, `requires_model` and `supported`. `supported` is `false` when the engine has no label for the object, `true`/`false` when the model labels of `server_id` are readable, and `null` (unknown) otherwise. The Frigate adapter implements `camconfig.ModelLabelSource`: it reads `model` from `GET /api/config` and returns Frigate's default COCO labelmap with the inline `model.labelmap` overrides applied. A `model.labelmap_path` (a file on the Frigate host) cannot be read, so it stays unknown, like an unreachable server. With `server_id` the caller needs servers.view on it (403/404 otherwise); the labels are cached for five minutes per server. Any other engine that implements `camconfig.ModelLabelSource` lights this up with no API change.
- Read side (`GET .../normalized-config`): `detection.objects` keeps the raw engine labels (unchanged, for audit and advanced search); `detection.selection` and `detection.categories` are the catalog view collapsed from them; `detection.raw` holds tracked labels the catalog does not know; `detection.available_labels` lists the model labels when the engine can report them (absent = unknown).
- Write side: patch `detection.selection` (object or category ids) instead of `detection.objects`. Sending both, or an unknown id, is a 400. The selection is expanded to engine labels with `ExpandForModel` and applied through the `detection.objects` path, so the audit trail stores the raw labels. Ids the model cannot honor (for example `vehicle.pickup` on Frigate) are listed in the review `unsupported` and the apply is rejected with 400; a category id (`vehicle`) skips members the engine cannot report instead of failing.
- Review lines for a selection patch use catalog labels: one per category that flips completely (`Vehículos`, from `false` to `true`) and one per object (`Camión`), besides the raw `detection.objects` line.
