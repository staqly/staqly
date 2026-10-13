// Command staqly is the user-facing staq.ly command line client.
package main

import (
	"os"

	"github.com/staqly/staqly/apps/cli/internal/cli"
)

// Set by the linker: -X main.version=… -X main.commit=… -X main.date=…
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	os.Exit(cli.Main(cli.BuildInfo{Version: version, Commit: commit, Date: date}, os.Args[1:]))
}
