-- +goose Up
-- Discord connects through the server's bot now ("discord": a Discord server, the post picks its channels). Every
-- Discord channel connected before is a channel webhook, which keeps working as its own platform, "discord_webhook":
-- same channel ids, credentials and posts, so nothing scheduled changes.
UPDATE channels SET provider = 'discord_webhook' WHERE provider = 'discord';

-- +goose Down
UPDATE channels SET provider = 'discord' WHERE provider = 'discord_webhook';
