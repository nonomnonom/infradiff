package mcp

import (
	"context"
	"sort"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/infradiff/infradiff/internal/model"
	"github.com/infradiff/infradiff/internal/parser"
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
type Risk struct {
	Severity string `json:"severity"`
	Node     string `json:"node"`
	Message  string `json:"message"`
}

// ExplainGraph parses one input and returns a structured summary.
func (w Workspace) ExplainGraph(_ context.Context, _ *sdk.CallToolRequest, args generateArgs) (*sdk.CallToolResult, Explain, error) {
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
	explain.Risks = risks(graph)
	return nil, explain, nil
}

var severityRank = map[string]int{"high": 0, "medium": 1, "low": 2}

func risks(graph *model.Graph) []Risk {
	out := []Risk{}
	for _, node := range graph.Nodes {
		metadata := node.Metadata
		switch node.Type {
		case model.NodeDatabase, model.NodeCache, model.NodeQueue:
			if metadata["ports"] != "" {
				out = append(out, Risk{
					Severity: "high",
					Node:     node.ID,
					Message:  "publishes host ports; keep data stores on a private network",
				})
			}
			if strings.EqualFold(metadata["publicly_accessible"], "true") {
				out = append(out, Risk{
					Severity: "high",
					Node:     node.ID,
					Message:  "is publicly accessible; restrict access to the private network",
				})
			}
		case model.NodeLoadBalancer:
			if kind := metadata["type"]; kind == "LoadBalancer" || kind == "NodePort" {
				out = append(out, Risk{
					Severity: "medium",
					Node:     node.ID,
					Message:  "is reachable from outside the cluster",
				})
			}
		}
		if image, ok := metadata["image"]; ok && !imagePinned(image) {
			out = append(out, Risk{
				Severity: "medium",
				Node:     node.ID,
				Message:  "uses an unpinned image tag; pin a version or digest",
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if severityRank[out[i].Severity] != severityRank[out[j].Severity] {
			return severityRank[out[i].Severity] < severityRank[out[j].Severity]
		}
		if out[i].Node != out[j].Node {
			return out[i].Node < out[j].Node
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// imagePinned reports whether an image reference names a version or digest
// rather than floating on latest.
func imagePinned(image string) bool {
	if strings.Contains(image, "@") {
		return true
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash {
		return false
	}
	tag := image[lastColon+1:]
	return tag != "" && tag != "latest"
}
