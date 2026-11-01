// Package api embeds the files the server serves: the OpenAPI document and the dashboard.
package api

import "embed"

// OpenAPI is the OpenAPI document served at /openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte

// Web is the static export of apps/platform (`make dashboard` copies apps/platform/out here;
// the Docker build does the same). In a plain `go build` it holds only
// .gitkeep and the web server explains how to build it.
//
//go:embed all:web
var Web embed.FS
