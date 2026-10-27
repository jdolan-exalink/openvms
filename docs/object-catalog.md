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

## Adding an engine mapping

Add an entry to the object's `Engine` map in `default.go` (`"provision": {"vehicle"}`). A label may belong to only one object per engine; `New` rejects duplicates and `TestEveryLabelBelongsToOneObject` guards the default data. An engine that reports only a coarse class (Provision: `vehicle`) maps it to the coarsest object that is honest, and leaves finer objects unmapped.

## Adding a category or object

Append to `Default()` in the position the UI should show it, with a unique id (`<category>.<name>`), then run `go test ./internal/camconfig/catalog/...`.
