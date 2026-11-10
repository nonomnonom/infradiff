package cli

import (
	"github.com/spf13/cobra"

	mcpserver "github.com/infradiff/infradiff/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp [workspace]",
		Short: "Run the Model Context Protocol server over stdio",
		Long: "Serve generate_diagram, diff_infrastructure, and explain_graph to an MCP client.\n" +
			"The optional workspace directory defaults to the current directory; tool paths\n" +
			"cannot escape it.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := "."
			if len(args) == 1 {
				root = args[0]
			}
			return mcpserver.Serve(cmd.Context(), root)
		},
	}
}
