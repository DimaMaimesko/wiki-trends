// Command wikitrends analyses Wikipedia pageview data for product decisions.
//
// It is the executable half of the wiki-trends Agent Skill. Run
// it with `go run ./cmd/wikitrends <command>`; there is nothing to install and no
// dependencies to download.
package main

import (
	"os"

	"github.com/dmytromaimesko/wiki-trends/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
