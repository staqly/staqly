-- +goose Up
-- Guest links (`server connect-link --for`): a sign-in link that works max_uses times (0 = until it expires) and
-- signs browsers in with guest_role ('' = an ordinary connect link); its sessions keep that role and end with the link.
ALTER TABLE device_links ADD COLUMN max_uses INTEGER NOT NULL DEFAULT 1;
ALTER TABLE device_links ADD COLUMN uses INTEGER NOT NULL DEFAULT 0;
ALTER TABLE device_links ADD COLUMN guest_role TEXT NOT NULL DEFAULT '';
UPDATE device_links SET uses = 1 WHERE used_at IS NOT NULL;
ALTER TABLE sessions ADD COLUMN guest_role TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN guest_role;
ALTER TABLE device_links DROP COLUMN guest_role;
ALTER TABLE device_links DROP COLUMN uses;
ALTER TABLE device_links DROP COLUMN max_uses;
