-- name: ListRules :many
SELECT id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version
FROM rules
WHERE tenant_id = @tenant_id
ORDER BY name ASC, created_at DESC;

-- name: GetRule :one
SELECT id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version
FROM rules
WHERE id = @id AND tenant_id = @tenant_id;

-- name: CreateRule :one
INSERT INTO rules (tenant_id, name, trigger_type, conditions, actions, enabled)
VALUES (@tenant_id, @name, @trigger_type, @conditions, @actions, @enabled)
RETURNING id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version;

-- name: UpdateRule :one
UPDATE rules
SET
  name = coalesce(sqlc.narg('name'), name),
  trigger_type = coalesce(sqlc.narg('trigger_type'), trigger_type),
  conditions = coalesce(sqlc.narg('conditions'), conditions),
  actions = coalesce(sqlc.narg('actions'), actions),
  enabled = coalesce(sqlc.narg('enabled'), enabled),
  version = version + 1,
  updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND version = @expected_version
RETURNING id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version;

-- name: DeleteRule :exec
DELETE FROM rules
WHERE id = @id AND tenant_id = @tenant_id;

-- name: ListActiveRulesByTrigger :many
SELECT id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at, version
FROM rules
WHERE tenant_id = @tenant_id AND trigger_type = @trigger_type AND enabled = true
ORDER BY created_at ASC;

-- name: InsertRuleVersion :exec
INSERT INTO rule_versions (rule_id, tenant_id, version, snapshot, changed_by)
VALUES (@rule_id, @tenant_id, @version, @snapshot, @changed_by);

-- name: ListRuleVersions :many
SELECT version, snapshot, changed_by, created_at
FROM rule_versions
WHERE tenant_id = @tenant_id AND rule_id = @rule_id
ORDER BY version DESC;

-- name: ListCameraGroupsForCamera :many
SELECT group_id FROM camera_group_members
WHERE tenant_id = @tenant_id AND camera_id = @camera_id
ORDER BY group_id;

-- name: CountCamerasByIDs :one
SELECT count(*) FROM cameras
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: CountCameraGroupsByIDs :one
SELECT count(*) FROM camera_groups
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: CountSitesByIDs :one
SELECT count(*) FROM sites
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: CountServersByIDs :one
SELECT count(*) FROM frigate_servers
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);

-- name: CountMapZonesByIDs :one
SELECT count(*) FROM map_zones
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[]);
