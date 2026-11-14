// Package dbfiles embeds the goose migrations, one set for SQLite (local mode) and PostgreSQL (cloud mode):
// written in SQLite's dialect, read by PostgreSQL through store.postgresDDL.
package dbfiles

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS
