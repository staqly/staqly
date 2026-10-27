-- +goose Up
-- oauth_clients: the OAuth clients staqly registered on its own on a platform's servers (Mastodon: one per server
-- and return address), so every sign-in to that server reuses one instead of registering again. Server-wide, not a
-- workspace's: the client is staqly's. credentials is sealed (client id and secret).
CREATE TABLE oauth_clients (
    provider     TEXT NOT NULL,
    server       TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    credentials  TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (provider, server, redirect_uri)
);

-- +goose Down
DROP TABLE oauth_clients;
