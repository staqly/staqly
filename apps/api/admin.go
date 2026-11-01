package api

import "embed"

// Admin is the static export of apps/admin (`make dashboard` copies apps/admin/out here; the Docker
// build does the same). In a plain `go build` it holds only .gitkeep.
//
//go:embed all:admin
var Admin embed.FS
