-- +goose Up
-- M3-3: GIN indexes for the zone and sub_label array filters on /events (PRD §46).
-- Finding (see odd/tasks/m3-search.md, M3-3 evidence, same root cause as the M3-2 plate
-- index): with FORCE ROW LEVEL SECURITY on events, EXPLAIN (enable_seqscan=off) still
-- chooses Seq Scan for `zones && ?` / `sub_labels && ?`, because the array-overlap operator's
-- support function (arrayoverlap) is not marked LEAKPROOF, so Postgres refuses to push the
-- qual below the RLS barrier into an index scan. Kept anyway per PRD §46; not harmful (a GIN
-- index adds write overhead but nothing worse), and they become usable if `events` ever loses
-- FORCE RLS or a leakproof wrapper is added, so they are not provably useless AND harmful.
CREATE INDEX events_zones_idx ON events USING gin (zones);
CREATE INDEX events_sub_labels_idx ON events USING gin (sub_labels);

-- +goose Down
DROP INDEX IF EXISTS events_sub_labels_idx;
DROP INDEX IF EXISTS events_zones_idx;
