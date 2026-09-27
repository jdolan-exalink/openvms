-- +goose Up
-- M5: saved multi-server live views (PRD §46-50). M6: exports requested through the VMS
-- (PRD §51-55); the video file stays in the origin Frigate.

CREATE TABLE views (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants (id),
    owner_id    uuid NOT NULL REFERENCES users (id),
    name        text NOT NULL,
    shared      boolean NOT NULL DEFAULT false,
    -- {"columns": 3, "cells": [{"camera_id": "…", "quality": "sub"}, null, …]}
    layout      jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);
CREATE INDEX views_tenant_idx ON views (tenant_id) WHERE deleted_at IS NULL;

CREATE TABLE exports (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (id),
    site_id            uuid NOT NULL REFERENCES sites (id),
    server_id          uuid NOT NULL REFERENCES frigate_servers (id),
    camera_id          uuid NOT NULL REFERENCES cameras (id),
    requested_by       uuid NOT NULL REFERENCES users (id),
    name               text NOT NULL,
    start_time         timestamptz NOT NULL,
    end_time           timestamptz NOT NULL,
    -- Export id inside Frigate.
    remote_id          text NOT NULL DEFAULT '',
    status             text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'ready', 'failed')),
    progress           real NOT NULL DEFAULT 0,
    error              text NOT NULL DEFAULT '',
    -- URL path of the finished file inside Frigate (e.g. /exports/x.mp4).
    remote_path        text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz
);
CREATE INDEX exports_tenant_time_idx ON exports (tenant_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX exports_active_idx ON exports (status) WHERE status IN ('pending', 'running') AND deleted_at IS NULL;

ALTER TABLE views ENABLE ROW LEVEL SECURITY;
ALTER TABLE views FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON views USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

ALTER TABLE exports ENABLE ROW LEVEL SECURITY;
ALTER TABLE exports FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON exports USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));

-- +goose Down
DROP TABLE IF EXISTS exports;
DROP TABLE IF EXISTS views;
