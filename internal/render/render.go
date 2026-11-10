// Package render turns an infrastructure graph into Mermaid, JSON, or HTML.
package render

import (
	"fmt"
	"strings"

	"github.com/infradiff/infradiff/internal/model"
)

// Format is an output format name.
type Format string

// Supported output formats.
const (
	FormatMermaid Format = "mermaid"
	FormatJSON    Format = "json"
	FormatHTML    Format = "html"
)

// ParseFormat validates a user-supplied format name.
func ParseFormat(value string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(value))) {
	case FormatMermaid:
		return FormatMermaid, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatHTML:
		return FormatHTML, nil
	default:
		return "", fmt.Errorf("unknown format %q; choose mermaid, json, or html", value)
	}
}

// Render dispatches to the requested renderer.
func Render(graph *model.Graph, format Format) ([]byte, error) {
	switch format {
	case FormatMermaid:
		return Mermaid(graph), nil
	case FormatJSON:
		return JSON(graph)
	case FormatHTML:
		return HTML(graph)
	default:
		return nil, fmt.Errorf("unknown format %q; choose mermaid, json, or html", format)
	}
}

// prepared returns a sorted copy with an up-to-date summary so renderers never
// mutate their input and always produce deterministic output.
func prepared(graph *model.Graph) *model.Graph {
	out := *graph
	out.Nodes = append([]model.Node(nil), graph.Nodes...)
	out.Edges = append([]model.Edge(nil), graph.Edges...)
	out.Groups = append([]model.Group(nil), graph.Groups...)
	out.Sort()
	out.Recount()
	return &out
}
