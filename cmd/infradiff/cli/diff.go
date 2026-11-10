package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/infradiff/infradiff/internal/diff"
	"github.com/infradiff/infradiff/internal/gitdiff"
	"github.com/infradiff/infradiff/internal/model"
	"github.com/infradiff/infradiff/internal/parser"
	"github.com/infradiff/infradiff/internal/render"
	"github.com/spf13/cobra"
)

var errChangesFound = errors.New("infrastructure changed")

func newDiffCmd() *cobra.Command {
	var format string
	var output string
	var gitRef string
	var failOnChange bool

	cmd := &cobra.Command{
		Use:   "diff <old> <new>",
		Short: "Show what changed between two infrastructure versions",
		Long: "Compare two directories or files and render added, removed, and changed\n" +
			"nodes and edges. With --git, compare the working tree against a revision.",
		RunE: func(cmd *cobra.Command, args []string) error {
			oldGraph, newGraph, err := loadDiffInputs(args, gitRef)
			if err != nil {
				return err
			}
			result := diff.Compare(oldGraph, newGraph)

			outFormat, err := render.ParseFormat(format)
			if err != nil {
				return err
			}
			data, err := render.Render(result, outFormat)
			if err != nil {
				return err
			}
			if output == "" {
				if _, err := cmd.OutOrStdout().Write(data); err != nil {
					return err
				}
			} else if err := writeFileAtomic(output, data); err != nil {
				return fmt.Errorf("write %s: %w", output, err)
			}

			printLine(cmd.ErrOrStderr(), result.Summary.Text)
			for _, line := range diff.ChangeLines(result) {
				printLine(cmd.ErrOrStderr(), line)
			}

			if failOnChange && hasChanges(result) {
				return errChangesFound
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "mermaid", "output format: mermaid, json, or html")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write output to this file instead of stdout")
	cmd.Flags().StringVar(&gitRef, "git", "", "compare the working tree against this git revision")
	cmd.Flags().BoolVar(&failOnChange, "fail-on-change", false, "exit with status 1 when anything changed")
	return cmd
}

func loadDiffInputs(args []string, gitRef string) (*model.Graph, *model.Graph, error) {
	if gitRef != "" {
		if len(args) > 1 {
			return nil, nil, fmt.Errorf("diff --git accepts at most one path, got %d", len(args))
		}
		path := "."
		if len(args) == 1 {
			path = args[0]
		}
		return loadGitSides(gitRef, path)
	}
	if len(args) != 2 {
		return nil, nil, fmt.Errorf("diff needs two paths, or use --git <ref> [path]")
	}
	oldGraph, err := parser.DefaultRegistry().ParsePath(args[0])
	if err != nil {
		return nil, nil, err
	}
	newGraph, err := parser.DefaultRegistry().ParsePath(args[1])
	if err != nil {
		return nil, nil, err
	}
	return oldGraph, newGraph, nil
}

func loadGitSides(ref, pathArg string) (*model.Graph, *model.Graph, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("diff --git: %w", err)
	}
	repoRoot, err := gitdiff.RepoRoot(cwd)
	if err != nil {
		return nil, nil, err
	}
	absPath, err := filepath.Abs(pathArg)
	if err != nil {
		return nil, nil, fmt.Errorf("diff --git: %w", err)
	}
	rel, err := filepath.Rel(repoRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, nil, fmt.Errorf("diff --git: %s is outside the repository %s", pathArg, repoRoot)
	}
	relSlash := filepath.ToSlash(rel)

	if err := gitdiff.VerifyRef(repoRoot, ref); err != nil {
		return nil, nil, err
	}

	newGraph := model.New()
	if _, statErr := os.Stat(absPath); statErr == nil {
		newGraph, err = parser.DefaultRegistry().ParsePath(absPath)
		if err != nil {
			return nil, nil, err
		}
	} else if !os.IsNotExist(statErr) {
		return nil, nil, fmt.Errorf("diff --git: cannot read %s: %w", absPath, statErr)
	}

	oldGraph := model.New()
	if gitdiff.PathExistsAtRef(repoRoot, ref, relSlash) {
		dir, cleanup, err := gitdiff.ExtractRef(repoRoot, ref, relSlash)
		if err != nil {
			return nil, nil, err
		}
		oldPath := filepath.Join(dir, filepath.FromSlash(relSlash))
		if relSlash == "." {
			oldPath = dir
		}
		oldGraph, err = parser.DefaultRegistry().ParsePath(oldPath)
		cleanup()
		if err != nil {
			return nil, nil, err
		}
	}

	return oldGraph, newGraph, nil
}

func hasChanges(graph *model.Graph) bool {
	counts := graph.Summary
	return counts.Nodes.Added+counts.Nodes.Removed+counts.Nodes.Changed+
		counts.Edges.Added+counts.Edges.Removed+counts.Edges.Changed > 0
}

func printLine(writer io.Writer, text string) {
	_, _ = fmt.Fprintln(writer, text)
}
