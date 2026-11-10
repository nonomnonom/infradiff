// Package diff compares two infrastructure graphs by stable IDs.
package diff

import (
	"fmt"
	"maps"
	"sort"
	"strings"

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
	renamed := detectRenames(oldGraph.Nodes, newGraph.Nodes, oldNodes, newNodes)

	for _, node := range newGraph.Nodes {
		status := nodeStatusFor(oldNodes[node.ID], node.ID, node)
		if status == model.StatusAdded && node.Status == model.StatusUnchanged {
			if _, ok := renamed[node.ID]; ok {
				status = model.StatusChanged
			}
		}
		nodeStatuses[node.ID] = status
		out := node
		out.Status = status
		result.Nodes = append(result.Nodes, out)
	}
	for _, node := range oldGraph.Nodes {
		if _, exists := newNodes[node.ID]; exists {
			continue
		}
		if _, exists := renamedReverse(renamed, node.ID); exists {
			continue
		}
		nodeStatuses[node.ID] = model.StatusRemoved
		out := node
		out.Status = model.StatusRemoved
		result.Nodes = append(result.Nodes, out)
	}

	result.Edges = compareEdges(oldGraph.Edges, newGraph.Edges, nodeStatuses, renamed)

	oldGroups := make(map[string]model.Group, len(oldGraph.Groups))
	for _, group := range oldGraph.Groups {
		oldGroups[group.ID] = group
	}
	for _, group := range newGraph.Groups {
		out := group
		if existing, ok := oldGroups[group.ID]; !ok {
			out.Status = model.StatusAdded
		} else if existing.Name != group.Name || existing.Kind != group.Kind ||
			!maps.Equal(existing.Metadata, group.Metadata) {
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

// detectRenames pairs a removed node with an added node only when the two are
// the sole candidates with the same type and metadata. Group is deliberately
// ignored so a rename or namespace move can still be recognized; the group
// itself is compared separately. Several identical resources are not paired.
type renameIndex map[string]map[string]string

func detectRenames(oldNodes, newNodes []model.Node, oldByID, newByID map[string]model.Node) renameIndex {
	type bucketKey string
	oldBuckets := make(map[bucketKey][]model.Node)
	newBuckets := make(map[bucketKey][]model.Node)
	for _, node := range oldNodes {
		if _, exists := newByID[node.ID]; exists {
			continue
		}
		key := bucketKey(renameKey(node))
		oldBuckets[key] = append(oldBuckets[key], node)
	}
	for _, node := range newNodes {
		if _, exists := oldByID[node.ID]; exists {
			continue
		}
		key := bucketKey(renameKey(node))
		newBuckets[key] = append(newBuckets[key], node)
	}

	renamed := make(renameIndex)
	for key, olds := range oldBuckets {
		news := newBuckets[key]
		if len(olds) != 1 || len(news) != 1 {
			continue
		}
		if renamed[news[0].ID] == nil {
			renamed[news[0].ID] = make(map[string]string)
		}
		renamed[news[0].ID][olds[0].ID] = news[0].ID
	}
	return renamed
}

func renameKey(node model.Node) string {
	keys := make([]string, 0, len(node.Metadata))
	for key, value := range node.Metadata {
		keys = append(keys, key+"\x00"+value)
	}
	sort.Strings(keys)
	return string(node.Type) + "\x00" + strings.Join(keys, "\x01")
}

func renamedReverse(renamed renameIndex, oldID string) (string, bool) {
	for newID, olds := range renamed {
		if _, ok := olds[oldID]; ok {
			return newID, true
		}
	}
	return "", false
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

func compareEdges(oldEdges, newEdges []model.Edge, nodeStatus map[string]model.Status, renamed renameIndex) []model.Edge {
	type key struct{ from, to string }
	oldByPair := make(map[key]model.Edge, len(oldEdges))
	for _, edge := range oldEdges {
		edge.From = renamedID(renamed, edge.From)
		edge.To = renamedID(renamed, edge.To)
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
	for pair, edge := range oldByPair {
		if seen[pair] {
			continue
		}
		out := edge
		out.Status = model.StatusRemoved
		result = append(result, out)
	}
	return result
}

func renamedID(renamed renameIndex, id string) string {
	for newID, olds := range renamed {
		for oldID := range olds {
			if oldID == id {
				return newID
			}
		}
	}
	return id
}

// ChangeLines returns one line per changed element, in sorted order.
func ChangeLines(graph *model.Graph) []string {
	lines := make([]string, 0, len(graph.Nodes)+len(graph.Edges)+len(graph.Groups))
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
	for _, group := range graph.Groups {
		if group.Status == model.StatusUnchanged {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s group %s (%s)",
			statusSymbol(group.Status), group.ID, group.Kind))
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
