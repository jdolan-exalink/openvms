-- name: ListRules :many
SELECT id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at
FROM rules
WHERE tenant_id = @tenant_id
ORDER BY name ASC, created_at DESC;

-- name: GetRule :one
SELECT id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at
FROM rules
WHERE id = @id AND tenant_id = @tenant_id;

-- name: CreateRule :one
INSERT INTO rules (tenant_id, name, trigger_type, conditions, actions, enabled)
VALUES (@tenant_id, @name, @trigger_type, @conditions, @actions, @enabled)
RETURNING id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at;

-- name: UpdateRule :one
UPDATE rules
SET
  name = coalesce(sqlc.narg('name'), name),
  trigger_type = coalesce(sqlc.narg('trigger_type'), trigger_type),
  conditions = coalesce(sqlc.narg('conditions'), conditions),
  actions = coalesce(sqlc.narg('actions'), actions),
  enabled = coalesce(sqlc.narg('enabled'), enabled),
  updated_at = now()
WHERE id = @id AND tenant_id = @tenant_id
RETURNING id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at;

-- name: DeleteRule :exec
DELETE FROM rules
WHERE id = @id AND tenant_id = @tenant_id;

-- name: ListActiveRulesByTrigger :many
SELECT id, tenant_id, name, trigger_type, conditions, actions, enabled, created_at, updated_at
FROM rules
WHERE tenant_id = @tenant_id AND trigger_type = @trigger_type AND enabled = true
ORDER BY created_at ASC;
