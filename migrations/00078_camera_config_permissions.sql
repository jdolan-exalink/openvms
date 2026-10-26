-- Granular camera configuration permissions (PRD §40). Every existing grant of the legacy
-- permission that allows the same action today is mirrored to the new permission at the same
-- subject, effect and scope, so nobody loses access (and, with the runtime compatibility rule in
-- internal/authz, nobody gains any). Denies are mirrored too: otherwise a subject allowed by a
-- group and denied directly would end up with an unrestricted new allow.
--   cameras.view                      -> cameras.config.view
--   servers.config                    -> cameras.config.edit/video/recording/analytics/advanced
--   cameras.manage                    -> cameras.config.ptz   (tours are gated by cameras.manage)
--   servers.restart                   -> cameras.restart
--   servers.manage + config.secrets   -> cameras.web.access   (same subject, same scope)
--   servers.config + config.secrets   -> cameras.config.yaml   (same subject, same scope)
-- +goose Up
INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
SELECT g.tenant_id, g.subject_type, g.subject_id, m.new_permission, g.effect, g.scope_type, g.scope_id, g.created_by
FROM permission_grants g
JOIN (VALUES
    ('cameras.view', 'cameras.config.view'),
    ('servers.config', 'cameras.config.edit'),
    ('servers.config', 'cameras.config.video'),
    ('servers.config', 'cameras.config.recording'),
    ('servers.config', 'cameras.config.analytics'),
    ('servers.config', 'cameras.config.advanced'),
    ('cameras.manage', 'cameras.config.ptz'),
    ('servers.restart', 'cameras.restart')
) AS m (legacy_permission, new_permission) ON m.legacy_permission = g.permission
ON CONFLICT DO NOTHING;

-- Pair permissions: an allow needs both legacy allows; a deny of either denies the new one.
INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
SELECT a.tenant_id, a.subject_type, a.subject_id, m.new_permission, a.effect, a.scope_type, a.scope_id, a.created_by
FROM permission_grants a
JOIN (VALUES
    ('servers.manage', 'servers.config.secrets', 'cameras.web.access'),
    ('servers.config', 'servers.config.secrets', 'cameras.config.yaml')
) AS m (first_permission, second_permission, new_permission) ON m.first_permission = a.permission
WHERE (a.effect = 'deny')
   OR EXISTS (
       SELECT 1 FROM permission_grants b
       WHERE b.permission = m.second_permission AND b.effect = 'allow' AND a.effect = 'allow'
         AND b.subject_type = a.subject_type AND b.subject_id = a.subject_id
         AND b.scope_type = a.scope_type AND b.scope_id IS NOT DISTINCT FROM a.scope_id)
ON CONFLICT DO NOTHING;

INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id, created_by)
SELECT b.tenant_id, b.subject_type, b.subject_id, m.new_permission, 'deny', b.scope_type, b.scope_id, b.created_by
FROM permission_grants b
JOIN (VALUES
    ('servers.config.secrets', 'cameras.web.access'),
    ('servers.config.secrets', 'cameras.config.yaml')
) AS m (legacy_permission, new_permission) ON m.legacy_permission = b.permission
WHERE b.effect = 'deny'
ON CONFLICT DO NOTHING;

-- +goose Down
-- Remove only grants this migration derives from a legacy grant of the same subject, effect and scope.
DELETE FROM permission_grants n
WHERE n.permission IN ('cameras.config.view', 'cameras.config.edit', 'cameras.config.video',
        'cameras.config.recording', 'cameras.config.analytics', 'cameras.config.advanced',
        'cameras.config.ptz', 'cameras.restart', 'cameras.web.access', 'cameras.config.yaml')
  AND EXISTS (
      SELECT 1 FROM permission_grants l
      JOIN (VALUES
          ('cameras.view', 'cameras.config.view'),
          ('servers.config', 'cameras.config.edit'),
          ('servers.config', 'cameras.config.video'),
          ('servers.config', 'cameras.config.recording'),
          ('servers.config', 'cameras.config.analytics'),
          ('servers.config', 'cameras.config.advanced'),
          ('cameras.manage', 'cameras.config.ptz'),
          ('servers.restart', 'cameras.restart'),
          ('servers.manage', 'cameras.web.access'),
          ('servers.config.secrets', 'cameras.web.access'),
          ('servers.config', 'cameras.config.yaml'),
          ('servers.config.secrets', 'cameras.config.yaml')
      ) AS m (legacy_permission, new_permission) ON m.legacy_permission = l.permission
      WHERE m.new_permission = n.permission AND l.effect = n.effect
        AND l.subject_type = n.subject_type AND l.subject_id = n.subject_id
        AND l.scope_type = n.scope_type AND l.scope_id IS NOT DISTINCT FROM n.scope_id
  );
