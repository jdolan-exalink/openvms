-- name: ListFaceClusterTenants :many
-- Tenants with an embedded pending face that no pass has grouped yet.
SELECT DISTINCT tenant_id
FROM face_sightings
WHERE review_status = 'pending' AND embedding IS NOT NULL AND cluster_id IS NULL;

-- name: ListFaceClusterInputs :many
SELECT id, COALESCE(identity_group_id, id) AS identity_id, embedding_profile, embedding_model, embedding,
       COALESCE(source_seen_at, first_seen_at)::timestamptz AS seen_at
FROM face_sightings
WHERE tenant_id = @tenant_id AND review_status = 'pending' AND embedding IS NOT NULL
ORDER BY first_seen_at DESC, id DESC;

-- name: SetFaceSightingClusters :exec
UPDATE face_sightings fs
SET cluster_id = v.cluster_id
FROM (SELECT s.id AS sighting_id, c.id AS cluster_id
      FROM unnest(@sighting_ids::uuid[]) WITH ORDINALITY AS s(id, n)
      JOIN unnest(@cluster_ids::uuid[]) WITH ORDINALITY AS c(id, n) ON c.n = s.n) AS v
WHERE fs.id = v.sighting_id AND fs.tenant_id = @tenant_id AND fs.review_status = 'pending'
  AND fs.cluster_id IS DISTINCT FROM v.cluster_id;

-- name: ListFaceUnknownClusters :many
-- One page of the pending faces grouped by their stored cluster. A face with no cluster is a group of one.
-- The recurrence rule and the sort are applied here; total is the number of groups before paging.
WITH base AS (
  SELECT COALESCE(fs.cluster_id, fs.id) AS cid, fs.id, COALESCE(fs.identity_group_id, fs.id) AS identity_id,
         fs.server_id, s.name AS server_name, fs.source_score,
         COALESCE(fs.source_seen_at, fs.first_seen_at)::timestamptz AS seen_at
  FROM face_sightings fs
  JOIN frigate_servers s ON s.id = fs.server_id AND s.tenant_id = fs.tenant_id
  WHERE fs.tenant_id = ANY(@tenant_ids::uuid[])
    AND fs.review_status = 'pending'
    AND (cardinality(@site_ids::uuid[]) = 0 OR s.site_id = ANY(@site_ids::uuid[]))
    AND (cardinality(@server_ids::uuid[]) = 0 OR fs.server_id = ANY(@server_ids::uuid[]))
), agg AS (
  SELECT cid,
         ((array_agg(id ORDER BY seen_at, id))[1])::uuid AS id,
         array_agg(id ORDER BY seen_at, id)::uuid[] AS sighting_ids,
         array_agg(DISTINCT identity_id)::uuid[] AS identity_ids,
         count(*)::int AS sighting_count,
         count(DISTINCT (seen_at AT TIME ZONE 'UTC')::date)::int AS distinct_days,
         min(seen_at)::timestamptz AS first_seen_at,
         max(seen_at)::timestamptz AS last_seen_at,
         (count(*) FILTER (WHERE seen_at >= now() - make_interval(days => @window_days::int)))::int AS in_window,
         ((array_agg(id ORDER BY source_score DESC NULLS LAST, seen_at, id))[1])::uuid AS best_sighting_id,
         COALESCE(max(source_score), 0)::float8 AS best_score,
         (max(source_score) IS NOT NULL)::boolean AS has_score,
         jsonb_agg(DISTINCT jsonb_build_object('id', server_id, 'name', server_name)) AS servers
  FROM base
  GROUP BY cid
)
SELECT agg.id, agg.sighting_ids, agg.identity_ids, agg.sighting_count, agg.distinct_days, agg.first_seen_at,
       agg.last_seen_at, agg.best_sighting_id, agg.best_score, agg.has_score, agg.servers,
       (@min_appearances::int > 0 AND agg.in_window >= @min_appearances::int)::boolean AS recurrent,
       (count(*) OVER ())::int AS total
FROM agg
ORDER BY
  CASE WHEN @sort::text = 'recent' THEN 0 WHEN @min_appearances::int > 0 AND agg.in_window >= @min_appearances::int THEN 1 ELSE 0 END DESC,
  CASE WHEN @sort::text = 'recent' THEN 0 ELSE agg.sighting_count END DESC,
  agg.last_seen_at DESC, agg.id
LIMIT @row_limit::int OFFSET @row_offset::int;
