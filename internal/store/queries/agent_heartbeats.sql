-- name: UpsertAgentHeartbeat :exec
-- One write per heartbeat. last_seen_at is the database clock. The composite foreign key
-- rejects a server that does not belong to the tenant, so a mismatched identity writes nothing.
INSERT INTO agent_heartbeats (server_id, tenant_id, last_seen_at, version, status)
VALUES (@server_id, @tenant_id, now(), @version, @status)
ON CONFLICT (server_id) DO UPDATE
SET last_seen_at = now(), version = EXCLUDED.version, status = EXCLUDED.status;
