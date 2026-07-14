// Command flakesift scores test flakiness from plain JUnit XML history
// and emits quarantine lists, trends, and CI gates.
package main

import (
	"os"

	"github.com/JaydenCJ/flakesift/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
