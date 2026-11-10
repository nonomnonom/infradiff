package model

import "fmt"

// Recount recomputes the summary from the current node and edge statuses.
func (g *Graph) Recount() {
	var nodes, edges Counts
	for _, node := range g.Nodes {
		countStatus(&nodes, node.Status)
	}
	for _, edge := range g.Edges {
		countStatus(&edges, edge.Status)
	}
	g.Summary = Summary{
		Nodes: nodes,
		Edges: edges,
		Text: fmt.Sprintf("+%d added, -%d removed, ~%d changed",
			nodes.Added+edges.Added,
			nodes.Removed+edges.Removed,
			nodes.Changed+edges.Changed),
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
