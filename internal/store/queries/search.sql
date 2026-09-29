-- name: SearchCameras :many
SELECT c.id, c.display_name, c.site_id, s.name AS site_name, c.location, c.status
FROM cameras c
JOIN sites s ON s.id = c.site_id
WHERE c.deleted_at IS NULL
  AND c.id = ANY(@camera_ids::uuid[])
  AND (
    c.display_name ILIKE '%' || @query::text || '%'
    OR c.location ILIKE '%' || @query::text || '%'
    OR @query::text = ANY(c.tags)
  )
ORDER BY c.display_name
LIMIT 5;

-- name: SearchSites :many
SELECT s.id, s.name,
       (SELECT count(*) FROM cameras c WHERE c.site_id = s.id AND c.deleted_at IS NULL)::int AS camera_count
FROM sites s
WHERE s.deleted_at IS NULL
  AND s.id = ANY(@site_ids::uuid[])
  AND s.name ILIKE '%' || @query::text || '%'
ORDER BY s.name
LIMIT 5;

-- name: SearchServers :many
SELECT fs.id, fs.name, fs.site_id, s.name AS site_name, fs.status
FROM frigate_servers fs
JOIN sites s ON s.id = fs.site_id
WHERE fs.deleted_at IS NULL
  AND fs.id = ANY(@server_ids::uuid[])
  AND fs.name ILIKE '%' || @query::text || '%'
ORDER BY fs.name
LIMIT 5;

-- name: SearchEvents :many
SELECT e.id, e.camera_id, c.display_name AS camera_name, s.name AS site_name,
       e.severity, e.labels, e.start_time, (e.thumb_key <> '') AS has_thumbnail
FROM events e
JOIN cameras c ON c.id = e.camera_id
JOIN sites s ON s.id = e.site_id
WHERE e.camera_id = ANY(@camera_ids::uuid[])
  AND (
    e.labels && ARRAY[@query::text]
    OR EXISTS (SELECT 1 FROM unnest(e.labels) l WHERE l ILIKE '%' || @query::text || '%')
    OR e.remote_id ILIKE '%' || @query::text || '%'
  )
ORDER BY e.start_time DESC
LIMIT 5;

-- name: SearchPlates :many
SELECT l.plate, l.camera_id, c.display_name AS camera_name, s.name AS site_name,
       l.seen_at,
       e.id AS event_id
FROM lpr_reads l
JOIN cameras c ON c.id = l.camera_id
JOIN sites s ON s.id = l.site_id
LEFT JOIN events e ON e.server_id = l.server_id AND l.remote_event_id = ANY(e.detection_ids)
WHERE l.camera_id = ANY(@camera_ids::uuid[])
  AND l.plate_normalized ILIKE '%' || @query::text || '%'
ORDER BY l.seen_at DESC
LIMIT 5;
