-- +goose Up
-- A charge is reserved before provider HTTP. Its original credit draws allow a definite
-- rejection to refund exactly those grants; crashes and ambiguous answers retain the charge.
CREATE TABLE publishing_allowance_reservations (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    ref TEXT NOT NULL,
    credit_draws TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(credit_draws)),
    PRIMARY KEY (workspace_id, ref)
);

-- +goose Down
DROP TABLE publishing_allowance_reservations;
