// Package diff compares two infrastructure graphs by stable IDs.
package diff

import (
	"fmt"
	"maps"

	"github.com/nonomnonom/infradiff/internal/model"
)

// Compare returns a new graph containing the union of both inputs, with every
// node, edge, and group marked added, removed, changed, or unchanged.
//
// Plan JSON statuses on the new graph take precedence: a resource the plan says
// will be removed is reported as removed even if it still appears in the old
// graph. Source file and line changes alone never mark a node as changed.
func Compare(oldGraph, newGraph *model.Graph) *model.Graph {
	if oldGraph == nil {
		oldGraph = model.New()
	}
	if newGraph == nil {
		newGraph = model.New()
	}

	result := model.New()
	result.SchemaVersion = model.SchemaVersion

	oldNodes := indexNodes(oldGraph.Nodes)
	newNodes := indexNodes(newGraph.Nodes)
	nodeStatuses := make(map[string]model.Status, len(newNodes)+len(oldNodes))

	for _, node := range newGraph.Nodes {
		status := nodeStatusFor(oldNodes[node.ID], node.ID, node)
		nodeStatuses[node.ID] = status
		out := node
		out.Status = status
		result.Nodes = append(result.Nodes, out)
	}
	for _, node := range oldGraph.Nodes {
		if _, exists := newNodes[node.ID]; exists {
			continue
		}
		nodeStatuses[node.ID] = model.StatusRemoved
		out := node
		out.Status = model.StatusRemoved
		result.Nodes = append(result.Nodes, out)
	}

	result.Edges = compareEdges(oldGraph.Edges, newGraph.Edges, nodeStatuses)

	oldGroups := make(map[string]model.Group, len(oldGraph.Groups))
	for _, group := range oldGraph.Groups {
		oldGroups[group.ID] = group
	}
	for _, group := range newGraph.Groups {
		out := group
		if existing, ok := oldGroups[group.ID]; !ok {
			out.Status = model.StatusAdded
		} else if existing.Name != group.Name || existing.Kind != group.Kind {
			out.Status = model.StatusChanged
		} else {
			out.Status = model.StatusUnchanged
		}
		result.Groups = append(result.Groups, out)
	}
	for _, group := range oldGraph.Groups {
		if _, exists := groupIn(newGraph.Groups, group.ID); exists {
			continue
		}
		out := group
		out.Status = model.StatusRemoved
		result.Groups = append(result.Groups, out)
	}

	result.Sort()
	result.Recount()
	return result
}

func nodeStatusFor(old model.Node, id string, current model.Node) model.Status {
	switch current.Status {
	case model.StatusRemoved:
		return model.StatusRemoved
	case model.StatusAdded, model.StatusChanged:
		return current.Status
	}
	if id == "" {
		return model.StatusAdded
	}
	if old.ID == "" {
		return model.StatusAdded
	}
	if old.Name != current.Name ||
		old.Type != current.Type ||
		old.Group != current.Group ||
		!maps.Equal(old.Metadata, current.Metadata) {
		return model.StatusChanged
	}
	return model.StatusUnchanged
}

func compareEdges(oldEdges, newEdges []model.Edge, nodeStatus map[string]model.Status) []model.Edge {
	type key struct{ from, to string }
	oldByPair := make(map[key]model.Edge, len(oldEdges))
	for _, edge := range oldEdges {
		oldByPair[key{edge.From, edge.To}] = edge
	}

	result := make([]model.Edge, 0, len(newEdges)+len(oldEdges))
	seen := make(map[key]bool, len(newEdges))
	for _, edge := range newEdges {
		pair := key{edge.From, edge.To}
		seen[pair] = true
		out := edge
		if previous, ok := oldByPair[pair]; !ok {
			out.Status = model.StatusAdded
		} else if previous.Kind != edge.Kind {
			out.Status = model.StatusChanged
		} else {
			out.Status = model.StatusUnchanged
		}
		if nodeStatus[out.From] == model.StatusRemoved || nodeStatus[out.To] == model.StatusRemoved {
			out.Status = model.StatusRemoved
		}
		result = append(result, out)
	}
	for _, edge := range oldEdges {
		if seen[key{edge.From, edge.To}] {
			continue
		}
		out := edge
		out.Status = model.StatusRemoved
		result = append(result, out)
	}
	return result
}

// ChangeLines returns one line per changed element, in sorted order.
func ChangeLines(graph *model.Graph) []string {
	lines := make([]string, 0, len(graph.Nodes)+len(graph.Edges))
	for _, node := range graph.Nodes {
		if node.Status == model.StatusUnchanged {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s node %s (%s)", statusSymbol(node.Status), node.ID, node.Type))
	}
	for _, edge := range graph.Edges {
		if edge.Status == model.StatusUnchanged {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s edge %s -> %s (%s)",
			statusSymbol(edge.Status), edge.From, edge.To, edge.Kind))
	}
	return lines
}

func statusSymbol(status model.Status) string {
	switch status {
	case model.StatusAdded:
		return "+"
	case model.StatusRemoved:
		return "-"
	case model.StatusChanged:
		return "~"
	default:
		return " "
	}
}

func indexNodes(nodes []model.Node) map[string]model.Node {
	index := make(map[string]model.Node, len(nodes))
	for _, node := range nodes {
		index[node.ID] = node
	}
	return index
}

func groupIn(groups []model.Group, id string) (model.Group, bool) {
	for _, group := range groups {
		if group.ID == id {
			return group, true
		}
	}
	return model.Group{}, false
}
