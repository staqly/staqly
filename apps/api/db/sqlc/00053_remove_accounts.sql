-- What migration 00053 (internal/store/migrate_00053.go, a Go migration) does to the schema, for sqlc: it reads
-- db/migrations and then this folder. Not embedded or run: the Go migration is.
ALTER TABLE workspaces DROP COLUMN account_id;
DROP TABLE accounts;
