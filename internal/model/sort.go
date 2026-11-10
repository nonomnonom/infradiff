package model

import "sort"

// Sort orders nodes, edges, and groups so that rendering and diffs are
// deterministic regardless of parser traversal order.
func (g *Graph) Sort() {
	sort.SliceStable(g.Nodes, func(i, j int) bool {
		return g.Nodes[i].ID < g.Nodes[j].ID
	})
	sort.SliceStable(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		if g.Edges[i].To != g.Edges[j].To {
			return g.Edges[i].To < g.Edges[j].To
		}
		return g.Edges[i].Kind < g.Edges[j].Kind
	})
	sort.SliceStable(g.Groups, func(i, j int) bool {
		return g.Groups[i].ID < g.Groups[j].ID
	})
}
