// Package dbfiles embeds the goose migrations.
package dbfiles

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS
