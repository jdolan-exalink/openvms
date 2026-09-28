-- +goose Up
-- PDW-7: a per-tenant configurable IANA time zone used to render the watermark's date/time
-- with its real UTC offset (e.g. "2026-09-28 10:05:30 -03:00"), replacing the fixed
-- "UTC+00:00" PDW-2/PDW-3 used as a placeholder (PlateRead itself carries no time zone; this
-- is a tenant-level display/burn-in setting, not per-camera). Defaults to
-- America/Argentina/Buenos_Aires (this deployment's home market) for any tenant that has not
-- configured one explicitly — same "missing row/column behaves like the default" shape as
-- owner_name's '' default from migration 00010.
ALTER TABLE tenant_branding ADD COLUMN timezone text NOT NULL DEFAULT 'America/Argentina/Buenos_Aires';

-- +goose Down
ALTER TABLE tenant_branding DROP COLUMN timezone;
