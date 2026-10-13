package api

import "embed"

// Web is the static export of apps/web (`make web` copies apps/web/out here;
// the Docker build does the same). In a plain `go build` it holds only
// .gitkeep and the web server explains how to build it.
//
//go:embed all:web
var Web embed.FS
