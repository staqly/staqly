-- +goose Up
-- What the audience answered to a published post on its channels (comments, replies), read from the platforms,
-- and the AI's reading of them per channel ('' = the whole post).
CREATE TABLE post_responses (
    workspace_id  TEXT NOT NULL,
    post_id       TEXT NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    channel_id    TEXT NOT NULL,
    id            TEXT NOT NULL,                       -- the platform's id
    parent_id     TEXT NOT NULL DEFAULT '',
    author        TEXT NOT NULL DEFAULT '',
    author_handle TEXT NOT NULL DEFAULT '',
    text          TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL DEFAULT '',
    likes         INTEGER,
    created_at    TEXT NOT NULL,
    fetched_at    TEXT NOT NULL,
    sentiment     TEXT NOT NULL DEFAULT '' CHECK (sentiment IN ('', 'positive', 'neutral', 'negative')),
    PRIMARY KEY (post_id, channel_id, id)
);
CREATE INDEX post_responses_fetched ON post_responses (workspace_id, fetched_at);

CREATE TABLE post_response_insights (
    workspace_id    TEXT NOT NULL,
    post_id         TEXT NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    channel_id      TEXT NOT NULL DEFAULT '',
    positive        INTEGER NOT NULL DEFAULT 0,
    neutral         INTEGER NOT NULL DEFAULT 0,
    negative        INTEGER NOT NULL DEFAULT 0,
    summary         TEXT NOT NULL DEFAULT '',
    themes          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(themes)),
    questions       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(questions)),
    status          TEXT NOT NULL DEFAULT '' CHECK (status IN ('', 'ok', 'waiting_credits', 'failed')),
    error           TEXT NOT NULL DEFAULT '',
    responses_seen  INTEGER NOT NULL DEFAULT 0,
    analyzed_at     TEXT NOT NULL,
    PRIMARY KEY (post_id, channel_id)
);

-- +goose Down
DROP TABLE post_response_insights;
DROP TABLE post_responses;
