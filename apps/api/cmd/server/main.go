// Command server runs the REST API, MCP and background workers (the
// staqly-api image). See package server.
package main

import (
	"os"

	"github.com/staqly/staqly/apps/api/server"
)

var version = "dev"

func main() {
	os.Exit(server.Main(os.Args[1:], version))
}
