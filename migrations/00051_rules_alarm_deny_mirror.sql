-- +goose Up
-- Alarms phase 2 (P2-03 follow-up): 00050 mirrored alarms.* allows into rules.read/rules.manage but not the
-- explicit denies, so a subject allowed alarms.manage by a role and denied it directly would still end up
-- with an unrestricted rules.manage allow. Mirror the denies too, with the same platform/tenant filter
-- (camera-scoped alarm grants have no rules equivalent):
--   alarms.manage (deny) -> deny of rules.manage and rules.read
--   alarms.view   (deny) -> deny of rules.read
INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
SELECT g.tenant_id, g.subject_type, g.subject_id, p.permission, 'deny', g.scope_type, g.scope_id, g.created_by
FROM permission_grants g
JOIN LATERAL (
    VALUES ('rules.manage'), ('rules.read')
) AS p (permission) ON true
WHERE g.scope_type IN ('platform', 'tenant') AND g.permission = 'alarms.manage' AND g.effect = 'deny'
ON CONFLICT DO NOTHING;

INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
SELECT g.tenant_id, g.subject_type, g.subject_id, 'rules.read', 'deny', g.scope_type, g.scope_id, g.created_by
FROM permission_grants g
WHERE g.scope_type IN ('platform', 'tenant') AND g.permission = 'alarms.view' AND g.effect = 'deny'
ON CONFLICT DO NOTHING;

-- +goose Down
-- Remove only the denies this migration mirrored; denies that 00050 derived from notifications.manage stay.
DELETE FROM permission_grants r
WHERE r.effect = 'deny' AND r.permission IN ('rules.manage', 'rules.read')
  AND r.scope_type IN ('platform', 'tenant')
  AND EXISTS (
      SELECT 1 FROM permission_grants a
      WHERE a.effect = 'deny'
        AND a.subject_type = r.subject_type AND a.subject_id = r.subject_id
        AND a.scope_type = r.scope_type
        AND a.scope_id IS NOT DISTINCT FROM r.scope_id
        AND (a.permission = 'alarms.manage' OR (a.permission = 'alarms.view' AND r.permission = 'rules.read'))
  )
  AND NOT EXISTS (
      SELECT 1 FROM permission_grants n
      WHERE n.effect = 'deny' AND n.permission = 'notifications.manage'
        AND n.subject_type = r.subject_type AND n.subject_id = r.subject_id
        AND n.scope_type = r.scope_type
        AND n.scope_id IS NOT DISTINCT FROM r.scope_id
  );
