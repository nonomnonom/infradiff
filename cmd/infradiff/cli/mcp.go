package cli

import (
	"os"

	"github.com/spf13/cobra"

	mcpserver "github.com/infradiff/infradiff/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the Model Context Protocol server over stdio",
		Long: "Serve generate_diagram, diff_infrastructure, and explain_graph to an MCP client.\n" +
			"The current directory is the workspace root; tool paths cannot escape it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := os.Getwd()
			if err != nil {
				return err
			}
			return mcpserver.Serve(cmd.Context(), root)
		},
	}
}
