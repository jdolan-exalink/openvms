-- Video codec of the live (sub) and hq (main) go2rtc streams of a camera, as go2rtc reports it
-- ("h264", "h265"). Empty means unknown: the stream has no producer yet, or go2rtc was not
-- reachable. Live view uses it to flag cameras whose codec browsers often cannot play.
-- +goose Up
ALTER TABLE cameras
    ADD COLUMN live_codec text NOT NULL DEFAULT '',
    ADD COLUMN hq_codec   text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cameras
    DROP COLUMN IF EXISTS live_codec,
    DROP COLUMN IF EXISTS hq_codec;
