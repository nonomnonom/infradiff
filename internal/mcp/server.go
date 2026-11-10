// Package mcp exposes infradiff as a Model Context Protocol server over stdio.
// Every tool is restricted to a workspace directory so a client cannot read
// arbitrary files from the machine.
package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Workspace bounds tool inputs to files below Root.
type Workspace struct {
	Root string
}

// NewServer builds an MCP server whose tools operate inside root. The returned
// server has no transport; call Run or Serve to connect it to a client.
func NewServer(root string) (*sdk.Server, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("mcp: resolving workspace %s: %w", root, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("mcp: workspace %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("mcp: workspace %s is not a directory", root)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("mcp: resolving workspace %s: %w", root, err)
	}

	workspace := Workspace{Root: resolved}
	server := sdk.NewServer(&sdk.Implementation{Name: "infradiff", Version: "dev"}, nil)

	sdk.AddTool(server, &sdk.Tool{
		Name: "generate_diagram",
		Description: "Parse Docker Compose, Terraform, or Kubernetes files and return a graph. " +
			"Formats: mermaid (default), json, html. Output is deterministic and secret values are redacted.",
	}, workspace.GenerateDiagram)

	sdk.AddTool(server, &sdk.Tool{
		Name: "diff_infrastructure",
		Description: "Compare two infrastructure versions and return the added, removed, and changed nodes and edges. " +
			"Formats: mermaid (default), json, html. Secret values are redacted.",
	}, workspace.DiffInfrastructure)

	sdk.AddTool(server, &sdk.Tool{
		Name: "explain_graph",
		Description: "Summarize an infrastructure graph: node and edge counts, groups, dependencies, and notable risks " +
			"such as data stores with published ports.",
	}, workspace.ExplainGraph)

	return server, nil
}

// Serve runs the MCP server over stdin/stdout until the client disconnects.
func Serve(ctx context.Context, root string) error {
	server, err := NewServer(root)
	if err != nil {
		return err
	}
	if err := server.Run(ctx, &sdk.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp: %w", err)
	}
	return nil
}

// resolve turns a tool path into an existing absolute path inside the
// workspace, rejecting escapes through ".." and symbolic links.
func (w Workspace) resolve(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("path is required")
	}
	candidate := input
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(w.Root, candidate)
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", input, err)
	}
	if !within(w.Root, absolute) {
		return "", fmt.Errorf("path %q is outside the workspace root %s", input, w.Root)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("path %q cannot be resolved inside the workspace (missing file, broken symbolic link, or broken directory junction): %w", input, err)
	}
	// Check again after resolving links so a link inside the workspace cannot
	// point at a file outside it.
	if !within(w.Root, resolved) {
		return "", fmt.Errorf("path %q is outside the workspace root %s", input, w.Root)
	}
	return resolved, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
