// Package dbfiles embeds the goose migrations: SQLite (local mode) and PostgreSQL (cloud mode).
package dbfiles

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS

// MigrationsPostgres mirrors Migrations for PostgreSQL: the same version numbers, so both reach the same
// schema (TestSchemasMatch). It starts at 00027, the SQLite schema at that version, as one migration.
//
//go:embed migrations-postgres/*.sql
var MigrationsPostgres embed.FS
