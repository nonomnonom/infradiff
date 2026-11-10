package mcp

import (
	"context"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nonomnonom/infradiff/internal/diff"
	"github.com/nonomnonom/infradiff/internal/parser"
	"github.com/nonomnonom/infradiff/internal/render"
)

type generateArgs struct {
	Path   string `json:"path" jsonschema:"file or directory to parse, relative to the MCP workspace root"`
	Format string `json:"format,omitempty" jsonschema:"output format: mermaid (default), json, or html"`
}

type diffArgs struct {
	OldPath string `json:"old_path" jsonschema:"file or directory with the old infrastructure version"`
	NewPath string `json:"new_path" jsonschema:"file or directory with the new infrastructure version"`
	Format  string `json:"format,omitempty" jsonschema:"output format: mermaid (default), json, or html"`
}

// GenerateDiagram parses one input and returns the rendered graph.
func (w Workspace) GenerateDiagram(_ context.Context, _ *sdk.CallToolRequest, args generateArgs) (*sdk.CallToolResult, any, error) {
	path, err := w.resolve(args.Path)
	if err != nil {
		return nil, nil, err
	}
	format, err := outputFormat(args.Format)
	if err != nil {
		return nil, nil, err
	}
	graph, err := parser.DefaultRegistry().ParsePath(path)
	if err != nil {
		return nil, nil, err
	}
	data, err := render.Render(graph, format)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(data)), nil, nil
}

// DiffInfrastructure compares two inputs and returns the difference.
func (w Workspace) DiffInfrastructure(_ context.Context, _ *sdk.CallToolRequest, args diffArgs) (*sdk.CallToolResult, any, error) {
	oldPath, err := w.resolve(args.OldPath)
	if err != nil {
		return nil, nil, fmt.Errorf("old_path: %w", err)
	}
	newPath, err := w.resolve(args.NewPath)
	if err != nil {
		return nil, nil, fmt.Errorf("new_path: %w", err)
	}
	format, err := outputFormat(args.Format)
	if err != nil {
		return nil, nil, err
	}
	oldGraph, err := parser.DefaultRegistry().ParsePath(oldPath)
	if err != nil {
		return nil, nil, err
	}
	newGraph, err := parser.DefaultRegistry().ParsePath(newPath)
	if err != nil {
		return nil, nil, err
	}
	result := diff.Compare(oldGraph, newGraph)
	data, err := render.Render(result, format)
	if err != nil {
		return nil, nil, err
	}
	return textResult(string(data)), nil, nil
}

func textResult(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: text}},
	}
}

func outputFormat(value string) (render.Format, error) {
	if value == "" {
		return render.FormatMermaid, nil
	}
	return render.ParseFormat(value)
}
