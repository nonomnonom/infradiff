package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/infradiff/infradiff/internal/parser"
	"github.com/infradiff/infradiff/internal/render"
	"github.com/infradiff/infradiff/internal/watch"
	"github.com/spf13/cobra"
)

func newGenerateCmd() *cobra.Command {
	var format string
	var output string
	var watchMode bool

	cmd := &cobra.Command{
		Use:   "generate <path>",
		Short: "Render an infrastructure diagram",
		Long: "Read Docker Compose, Terraform, or Kubernetes files at <path> and\n" +
			"render the resulting architecture as Mermaid, JSON, or a self-contained\n" +
			"HTML page. With --watch, regenerate whenever a source file changes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outFormat, err := render.ParseFormat(format)
			if err != nil {
				return err
			}

			renderOnce := func() error {
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
			}

			if err := renderOnce(); err != nil {
				return err
			}
			if !watchMode {
				return nil
			}

			printLine(cmd.ErrOrStderr(), "watching "+args[0]+" (Ctrl+C to stop)")
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			var ignoredOutput string
			if output != "" {
				ignoredOutput, _ = filepath.Abs(output)
			}
			return watch.Run(ctx, watch.Options{
				Root:     args[0],
				OnChange: renderOnce,
				OnError: func(err error) {
					printLine(cmd.ErrOrStderr(), "error: "+err.Error())
				},
				Ignore: func(path string) bool {
					if ignoredOutput == "" {
						return false
					}
					absPath, err := filepath.Abs(path)
					if err != nil {
						return false
					}
					return filepath.Clean(absPath) == filepath.Clean(ignoredOutput)
				},
			})
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "mermaid", "output format: mermaid, json, or html")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write output to this file instead of stdout")
	cmd.Flags().BoolVar(&watchMode, "watch", false, "regenerate whenever an input file changes")
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
