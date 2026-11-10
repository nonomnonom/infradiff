package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nonomnonom/infradiff/internal/model"
	"github.com/nonomnonom/infradiff/internal/parser"
	"github.com/nonomnonom/infradiff/internal/risk"
)

// Explain is the structured result of the explain_graph tool.
type Explain struct {
	SchemaVersion int            `json:"schema_version"`
	Summary       string         `json:"summary"`
	NodeCount     int            `json:"node_count"`
	EdgeCount     int            `json:"edge_count"`
	Groups        []GroupSummary `json:"groups"`
	NodesByType   map[string]int `json:"nodes_by_type"`
	Dependencies  []Dependency   `json:"dependencies"`
	Risks         []Risk         `json:"risks"`
}

// GroupSummary counts the nodes in one group.
type GroupSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Nodes int    `json:"nodes"`
}

// Dependency is one edge of the graph.
type Dependency struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// Risk is a notable configuration issue found in the graph.
type Risk = risk.Risk

type explainArgs struct {
	Path string `json:"path" jsonschema:"file or directory to parse, relative to the MCP workspace root"`
}

// ExplainGraph parses one input and returns a structured summary.
func (w Workspace) ExplainGraph(_ context.Context, _ *sdk.CallToolRequest, args explainArgs) (*sdk.CallToolResult, Explain, error) {
	path, err := w.resolve(args.Path)
	if err != nil {
		return nil, Explain{}, err
	}
	graph, err := parser.DefaultRegistry().ParsePath(path)
	if err != nil {
		return nil, Explain{}, err
	}

	explain := Explain{
		SchemaVersion: model.SchemaVersion,
		Summary:       graph.Summary.Text,
		NodeCount:     len(graph.Nodes),
		EdgeCount:     len(graph.Edges),
		Groups:        []GroupSummary{},
		NodesByType:   map[string]int{},
		Dependencies:  []Dependency{},
		Risks:         []Risk{},
	}

	groupNodes := make(map[string]int, len(graph.Groups))
	for _, node := range graph.Nodes {
		explain.NodesByType[string(node.Type)]++
		if node.Group != "" {
			groupNodes[node.Group]++
		}
	}
	for _, group := range graph.Groups {
		explain.Groups = append(explain.Groups, GroupSummary{
			ID:    group.ID,
			Name:  group.Name,
			Kind:  group.Kind,
			Nodes: groupNodes[group.ID],
		})
	}
	for _, edge := range graph.Edges {
		explain.Dependencies = append(explain.Dependencies, Dependency{
			From: edge.From,
			To:   edge.To,
			Kind: string(edge.Kind),
		})
	}
	explain.Risks = risk.Graph(graph)
	return nil, explain, nil
}
