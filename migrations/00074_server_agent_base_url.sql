-- Edge agent 0.4.0: an agent may be reached through a published address (a VPN name, a Zero Trust
-- tunnel) instead of host:port on the LAN. base_url is that address as http(s)://host[:port]; empty
-- means "use host and port as before", so every existing row keeps working unchanged.
-- +goose Up
ALTER TABLE server_agents ADD COLUMN base_url text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE server_agents DROP COLUMN IF EXISTS base_url;
