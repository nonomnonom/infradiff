package model

import "fmt"

// Recount recomputes the summary from the current node and edge statuses.
func (g *Graph) Recount() {
	var nodes, edges, groups Counts
	for _, node := range g.Nodes {
		countStatus(&nodes, node.Status)
	}
	for _, edge := range g.Edges {
		countStatus(&edges, edge.Status)
	}
	for _, group := range g.Groups {
		countStatus(&groups, group.Status)
	}
	g.Summary = Summary{
		Nodes: nodes,
		Edges: edges,
		Text: fmt.Sprintf("+%d added, -%d removed, ~%d changed",
			nodes.Added+edges.Added+groups.Added,
			nodes.Removed+edges.Removed+groups.Removed,
			nodes.Changed+edges.Changed+groups.Changed),
	}
}

func countStatus(counts *Counts, status Status) {
	switch status {
	case StatusAdded:
		counts.Added++
	case StatusRemoved:
		counts.Removed++
	case StatusChanged:
		counts.Changed++
	default:
		counts.Unchanged++
	}
}
