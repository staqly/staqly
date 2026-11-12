-- +goose Up
-- Sign-in methods a person removed. A verified address signs into the account that has it and adds a
-- method the account never had; one the person removed stays removed (the address alone must not bring it
-- back) until they add it again while signed in.
CREATE TABLE removed_identities (
    provider   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    removed_at TEXT NOT NULL,
    PRIMARY KEY (provider, subject, user_id)
);

CREATE INDEX removed_identities_user ON removed_identities (user_id);

-- +goose Down
DROP TABLE removed_identities;
