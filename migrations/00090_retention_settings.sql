-- +goose Up
-- Platform-wide retention, one row (not per tenant, so no RLS). Days: 0 keeps forever. A NULL
-- notification value means "use the NOTIFY_*_RETENTION environment default".
CREATE TABLE retention_settings (
    singleton                  boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    face_unknown_days          integer NOT NULL DEFAULT 30 CHECK (face_unknown_days BETWEEN 0 AND 3650),
    notification_delivery_days integer CHECK (notification_delivery_days BETWEEN 0 AND 3650),
    notification_read_days     integer CHECK (notification_read_days BETWEEN 0 AND 3650),
    pressure_cleanup           boolean NOT NULL DEFAULT true,
    pressure_percent           integer NOT NULL DEFAULT 85 CHECK (pressure_percent BETWEEN 50 AND 99),
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    updated_by                 uuid REFERENCES users (id) ON DELETE SET NULL
);
INSERT INTO retention_settings (singleton) VALUES (true);

-- +goose Down
DROP TABLE IF EXISTS retention_settings;
