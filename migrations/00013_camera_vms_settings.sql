-- +goose Up
-- S2-5: VMS-owned camera settings. These live only in OpenVMS (Frigate never sees them) and
-- the Frigate inventory sync must not overwrite them: UpsertCamera's DO UPDATE clause lists
-- its columns explicitly and leaves these alone. default_live_quality is the stream a camera
-- gets when it is added to a Live grid tile; tags/location/description feed search later.
ALTER TABLE cameras
    ADD COLUMN default_live_quality text NOT NULL DEFAULT 'sub' CHECK (default_live_quality IN ('sub', 'main')),
    ADD COLUMN description text NOT NULL DEFAULT '',
    ADD COLUMN location text NOT NULL DEFAULT '',
    ADD COLUMN tags text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE cameras
    DROP COLUMN tags,
    DROP COLUMN location,
    DROP COLUMN description,
    DROP COLUMN default_live_quality;
