package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nonomnonom/infradiff/internal/model"
)

// FilterOptions narrows a large graph for a readable diagram.
type FilterOptions struct {
	Focus string
	Depth int
	Hide  []string
}

// Filter returns a copy containing only nodes within Depth edges of Focus and
// removes nodes whose ID, name, or type is listed in Hide. Edges and groups are
// kept only when they still have both endpoints or a visible node.
func Filter(graph *model.Graph, options FilterOptions) (*model.Graph, error) {
	hidden := make(map[string]bool)
	for _, value := range options.Hide {
		for _, item := range strings.Split(value, ",") {
			item = strings.ToLower(strings.TrimSpace(item))
			if item != "" {
				hidden[item] = true
			}
		}
	}

	selected := make(map[string]bool)
	if options.Focus == "" {
		for _, node := range graph.Nodes {
			if !isHidden(node, hidden) {
				selected[node.ID] = true
			}
		}
	} else {
		focus, err := findNode(graph, options.Focus)
		if err != nil {
			return nil, err
		}
		depth := options.Depth
		if depth < 0 {
			return nil, fmt.Errorf("depth must be zero or greater")
		}
		selected = withinDepth(graph, focus.ID, depth)
		for id := range selected {
			node, ok := graph.Node(id)
			if ok && isHidden(*node, hidden) {
				delete(selected, id)
			}
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("the filter selected no nodes; widen --depth or remove --hide")
	}

	out := model.New()
	groupIDs := make(map[string]bool)
	for _, node := range graph.Nodes {
		if !selected[node.ID] {
			continue
		}
		out.Nodes = append(out.Nodes, node)
		if node.Group != "" {
			groupIDs[node.Group] = true
		}
	}
	for _, group := range graph.Groups {
		if groupIDs[group.ID] {
			out.Groups = append(out.Groups, group)
		}
	}
	for _, edge := range graph.Edges {
		if selected[edge.From] && selected[edge.To] {
			out.Edges = append(out.Edges, edge)
		}
	}
	out.Sort()
	out.Recount()
	return out, nil
}

func findNode(graph *model.Graph, focus string) (model.Node, error) {
	for _, node := range graph.Nodes {
		if node.ID == focus {
			return node, nil
		}
	}
	var matches []model.Node
	for _, node := range graph.Nodes {
		if node.Name == focus {
			matches = append(matches, node)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		ids := make([]string, 0, len(matches))
		for _, node := range matches {
			ids = append(ids, node.ID)
		}
		sort.Strings(ids)
		return model.Node{}, fmt.Errorf("focus %q is ambiguous; use one of: %s", focus, strings.Join(ids, ", "))
	}
	return model.Node{}, fmt.Errorf("focus %q is not a node id or name", focus)
}

func isHidden(node model.Node, hidden map[string]bool) bool {
	if len(hidden) == 0 {
		return false
	}
	return hidden[strings.ToLower(node.ID)] ||
		hidden[strings.ToLower(node.Name)] ||
		hidden[strings.ToLower(string(node.Type))]
}

func withinDepth(graph *model.Graph, focus string, depth int) map[string]bool {
	adjacent := make(map[string][]string)
	for _, edge := range graph.Edges {
		adjacent[edge.From] = append(adjacent[edge.From], edge.To)
		adjacent[edge.To] = append(adjacent[edge.To], edge.From)
	}
	selected := map[string]bool{focus: true}
	frontier := []string{focus}
	for level := 0; level < depth; level++ {
		var next []string
		for _, id := range frontier {
			for _, neighbor := range adjacent[id] {
				if selected[neighbor] {
					continue
				}
				selected[neighbor] = true
				next = append(next, neighbor)
			}
		}
		frontier = next
	}
	return selected
}
