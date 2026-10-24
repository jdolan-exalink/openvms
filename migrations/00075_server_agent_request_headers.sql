-- Edge agent 0.4.0: an agent published behind a Zero Trust gateway may need extra request headers on
-- every call (the service token of the gateway). They are credentials, so they are sealed like the
-- agent token (AES-GCM, bound to the server), never returned by the API, and empty for every
-- existing row.
-- +goose Up
ALTER TABLE server_agents ADD COLUMN request_headers_sealed bytea;

-- +goose Down
ALTER TABLE server_agents DROP COLUMN IF EXISTS request_headers_sealed;
