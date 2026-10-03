-- +goose Up
-- A long detection is a truck with a trailer. The trailer color is stored
-- beside the cab color. Empty means the vehicle is not articulated.

ALTER TABLE vehicle_attributes
    ADD COLUMN trailer_color text NOT NULL DEFAULT '',
    ADD COLUMN trailer_color_confidence real NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE vehicle_attributes
    DROP COLUMN IF EXISTS trailer_color_confidence,
    DROP COLUMN IF EXISTS trailer_color;
