// Package cli wires the infradiff commands. Commands are constructed per call
// so tests can exercise them without shared flag state.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Execute runs the CLI and returns a process exit code.
func Execute(args []string, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		if errors.Is(err, errChangesFound) {
			return 1
		}
		// Reporting the failure is best effort; the exit code still signals it.
		_, _ = fmt.Fprintf(stderr, "infradiff: %v\n", err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "infradiff",
		Short:         "See what your infrastructure change actually does, as a diagram",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}
	cmd.AddCommand(newGenerateCmd(), newDiffCmd(), newVersionCmd())
	return cmd
}
