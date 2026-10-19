// Package api embeds the OpenAPI document served at /openapi.yaml.
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPI []byte
