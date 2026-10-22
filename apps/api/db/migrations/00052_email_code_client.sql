-- +goose Up
-- Who asked for an email code (a keyed hash of the client address, never the address): the send limits count
-- per address and asker, so nobody can use up another person's codes for the day.
ALTER TABLE email_codes ADD COLUMN client TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE email_codes DROP COLUMN client;
