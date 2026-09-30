-- +goose Up
-- Notifications only carried a generic link ('/cameras', '/servers', '/alarms', or
-- '/events?selected=<id>'), so the ones tied to a server or camera could not be found when the
-- server is hard deleted. Rules now record the resource they fired for. No foreign keys: the
-- delete path removes these rows explicitly (and counts them in the audit entry), and history
-- rows must not block or silently vanish.
ALTER TABLE notifications
    ADD COLUMN server_id uuid,
    ADD COLUMN camera_id uuid;

CREATE INDEX idx_notifications_server ON notifications (server_id) WHERE server_id IS NOT NULL;
CREATE INDEX idx_notifications_camera ON notifications (camera_id) WHERE camera_id IS NOT NULL;

-- Backfill the event-triggered rows, whose link is exactly '/events?selected=<event id>'.
-- Older offline notifications have no id in the link and stay unlinked.
UPDATE notifications n
SET server_id = e.server_id, camera_id = e.camera_id
FROM events e
WHERE n.link = '/events?selected=' || e.id::text;

-- +goose Down
DROP INDEX IF EXISTS idx_notifications_camera;
DROP INDEX IF EXISTS idx_notifications_server;
ALTER TABLE notifications DROP COLUMN IF EXISTS camera_id, DROP COLUMN IF EXISTS server_id;
