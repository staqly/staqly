-- +goose Up
-- Webhook calls already handled (Kick sends each event at least once, with an id): a repeat is skipped. Kept a week.
CREATE TABLE inbound_events (
    provider    TEXT NOT NULL,
    id          TEXT NOT NULL,
    received_at TEXT NOT NULL,
    PRIMARY KEY (provider, id)
);

CREATE INDEX inbound_events_received ON inbound_events (received_at);

-- +goose Down
DROP TABLE inbound_events;
