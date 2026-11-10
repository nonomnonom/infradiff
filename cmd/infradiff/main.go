// Command infradiff reads infrastructure-as-code, renders a diagram, and shows
// what changed between two versions.
package main

import (
	"os"

	"github.com/infradiff/infradiff/cmd/infradiff/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
