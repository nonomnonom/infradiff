package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/infradiff/infradiff/internal/parser"
	"github.com/infradiff/infradiff/internal/render"
	"github.com/spf13/cobra"
)

func newGenerateCmd() *cobra.Command {
	var format string
	var output string

	cmd := &cobra.Command{
		Use:   "generate <path>",
		Short: "Render an infrastructure diagram",
		Long: "Read Docker Compose, Terraform, or Kubernetes files at <path> and\n" +
			"render the resulting architecture as a Mermaid diagram or JSON.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outFormat, err := render.ParseFormat(format)
			if err != nil {
				return err
			}
			graph, err := parser.DefaultRegistry().ParsePath(args[0])
			if err != nil {
				return err
			}
			data, err := render.Render(graph, outFormat)
			if err != nil {
				return err
			}
			if output == "" {
				_, err = cmd.OutOrStdout().Write(data)
				return err
			}
			if err := writeFileAtomic(output, data); err != nil {
				return fmt.Errorf("write %s: %w", output, err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "mermaid", "output format: mermaid, json, or html")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write output to this file instead of stdout")
	return cmd
}

// writeFileAtomic writes through a temporary file in the same directory so a
// failed render never leaves a half-written output file behind.
func writeFileAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".infradiff-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
