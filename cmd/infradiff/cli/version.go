package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is intentionally "dev" until the project decides on releases.
const version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the infradiff version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	}
}
