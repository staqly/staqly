-- +goose Up
-- Instagram (Facebook Business), the classic Facebook dialog that reached an account through its Facebook Page, is
-- now Instagram (Professional): Unified login as its own tile next to Instagram login. Channels connected the old
-- way keep their Page token and keep publishing; reconnecting one signs in with Unified login.
UPDATE channels SET provider = 'instagram_unified' WHERE provider = 'instagram_business';
UPDATE provider_apps SET provider = 'instagram_unified' WHERE provider = 'instagram_business';
UPDATE removed_identities SET provider = 'instagram_unified' WHERE provider = 'instagram_business';

-- +goose Down
UPDATE channels SET provider = 'instagram_business' WHERE provider = 'instagram_unified';
UPDATE provider_apps SET provider = 'instagram_business' WHERE provider = 'instagram_unified';
UPDATE removed_identities SET provider = 'instagram_business' WHERE provider = 'instagram_unified';
