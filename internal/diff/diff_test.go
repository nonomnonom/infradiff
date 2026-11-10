package diff

import (
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/infradiff/infradiff/internal/model"
	"github.com/infradiff/infradiff/internal/parser"
)

func TestCompareFixtures(t *testing.T) {
	oldGraph, err := parser.DefaultRegistry().ParsePath("../../testdata/compose/before")
	if err != nil {
		t.Fatalf("parse before: %v", err)
	}
	newGraph, err := parser.DefaultRegistry().ParsePath("../../testdata/compose/after")
	if err != nil {
		t.Fatalf("parse after: %v", err)
	}

	result := Compare(oldGraph, newGraph)

	wantNodes := map[string]model.Status{
		"compose:service:api":     model.StatusChanged,
		"compose:service:cache":   model.StatusAdded,
		"compose:service:worker":  model.StatusRemoved,
		"compose:service:db":      model.StatusUnchanged,
		"compose:network:backend": model.StatusUnchanged,
		"compose:volume:dbdata":   model.StatusUnchanged,
	}
	for id, want := range wantNodes {
		node, ok := result.Node(id)
		if !ok {
			t.Fatalf("result is missing node %q", id)
		}
		if node.Status != want {
			t.Fatalf("%s status = %q, want %q", id, node.Status, want)
		}
	}

	wantEdges := map[string]model.Status{
		"compose:service:cache->compose:network:backend":  model.StatusAdded,
		"compose:service:worker->compose:network:backend": model.StatusRemoved,
		"compose:service:api->compose:service:db":         model.StatusUnchanged,
		"compose:service:api->compose:network:backend":    model.StatusUnchanged,
	}
	for key, want := range wantEdges {
		from, to, _ := strings.Cut(key, "->")
		status, ok := edgeStatus(result, from, to)
		if !ok {
			t.Fatalf("result is missing edge %s", key)
		}
		if status != want {
			t.Fatalf("edge %s status = %q, want %q", key, status, want)
		}
	}

	if result.Summary.Text != "+2 added, -2 removed, ~1 changed" {
		t.Fatalf("summary = %q", result.Summary.Text)
	}
	if len(result.Groups) != 1 || result.Groups[0].Status != model.StatusUnchanged {
		t.Fatalf("groups = %+v, want one unchanged group", result.Groups)
	}
}

func TestCompareIdentical(t *testing.T) {
	graph, err := parser.DefaultRegistry().ParsePath("../../testdata/compose/before")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result := Compare(graph, graph)
	if result.Summary.Text != "+0 added, -0 removed, ~0 changed" {
		t.Fatalf("summary = %q", result.Summary.Text)
	}
	for _, node := range result.Nodes {
		if node.Status != model.StatusUnchanged {
			t.Fatalf("%s status = %q, want unchanged", node.ID, node.Status)
		}
	}
	if lines := ChangeLines(result); len(lines) != 0 {
		t.Fatalf("ChangeLines() = %v, want none", lines)
	}
}

func TestCompareSourceOnlyChangeIsNotAChange(t *testing.T) {
	build := func(line int, file string) *model.Graph {
		g := model.New()
		g.Nodes = append(g.Nodes, model.Node{
			ID: "a", Name: "a", Type: model.NodeService,
			Source:   model.Source{File: file, Line: line},
			Metadata: map[string]string{"image": "shop/a"},
			Status:   model.StatusUnchanged,
		})
		return g
	}

	result := Compare(build(3, "before.yaml"), build(9, "after.yaml"))
	if result.Nodes[0].Status != model.StatusUnchanged {
		t.Fatalf("status = %q, want unchanged for a moved source line", result.Nodes[0].Status)
	}
}

func TestCompareNodeChanges(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.Node)
		want   model.Status
	}{
		{"metadata", func(n *model.Node) { n.Metadata["image"] = "shop/a:2" }, model.StatusChanged},
		{"type", func(n *model.Node) { n.Type = model.NodeDatabase }, model.StatusChanged},
		{"name", func(n *model.Node) { n.Name = "b" }, model.StatusChanged},
		{"group", func(n *model.Node) { n.Group = "group:other" }, model.StatusChanged},
		{"unchanged", func(*model.Node) {}, model.StatusUnchanged},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldGraph := model.New()
			oldGraph.Nodes = append(oldGraph.Nodes, model.Node{
				ID: "a", Name: "a", Type: model.NodeService,
				Source:   model.Source{File: "a.yaml", Line: 1},
				Metadata: map[string]string{"image": "shop/a"},
				Status:   model.StatusUnchanged,
			})
			newGraph := model.New()
			node := oldGraph.Nodes[0]
			node.Metadata = maps.Clone(node.Metadata)
			tt.mutate(&node)
			newGraph.Nodes = append(newGraph.Nodes, node)

			result := Compare(oldGraph, newGraph)
			if result.Nodes[0].Status != tt.want {
				t.Fatalf("status = %q, want %q", result.Nodes[0].Status, tt.want)
			}
		})
	}
}

func TestCompareEdgeKindChange(t *testing.T) {
	build := func(kind model.EdgeKind) *model.Graph {
		g := model.New()
		for _, id := range []string{"a", "b"} {
			g.Nodes = append(g.Nodes, model.Node{
				ID: id, Name: id, Type: model.NodeService,
				Source:   model.Source{File: "a.yaml", Line: 1},
				Metadata: map[string]string{}, Status: model.StatusUnchanged,
			})
		}
		g.Edges = append(g.Edges, model.Edge{From: "a", To: "b", Kind: kind, Status: model.StatusUnchanged})
		return g
	}

	result := Compare(build(model.EdgeNetwork), build(model.EdgeDependsOn))
	if result.Edges[0].Status != model.StatusChanged {
		t.Fatalf("edge status = %q, want changed", result.Edges[0].Status)
	}
}

func TestComparePlanStatusWins(t *testing.T) {
	oldGraph := model.New()
	oldGraph.Nodes = append(oldGraph.Nodes, model.Node{
		ID: "a", Name: "a", Type: model.NodeService,
		Source:   model.Source{File: "a.tf", Line: 1},
		Metadata: map[string]string{}, Status: model.StatusUnchanged,
	})

	newGraph := model.New()
	newGraph.Nodes = append(newGraph.Nodes, model.Node{
		ID: "a", Name: "a", Type: model.NodeService,
		Source:   model.Source{File: "plan.json", Line: 0},
		Metadata: map[string]string{}, Status: model.StatusRemoved,
	})

	result := Compare(oldGraph, newGraph)
	if result.Nodes[0].Status != model.StatusRemoved {
		t.Fatalf("status = %q, want plan status removed", result.Nodes[0].Status)
	}
}

func TestCompareEdgeToRemovedNodeBecomesRemoved(t *testing.T) {
	build := func(bStatus model.Status) *model.Graph {
		g := model.New()
		g.Nodes = append(g.Nodes,
			model.Node{
				ID: "a", Name: "a", Type: model.NodeService,
				Source:   model.Source{File: "a.yaml", Line: 1},
				Metadata: map[string]string{}, Status: model.StatusUnchanged,
			},
			model.Node{
				ID: "b", Name: "b", Type: model.NodeService,
				Source:   model.Source{File: "a.yaml", Line: 2},
				Metadata: map[string]string{}, Status: bStatus,
			},
		)
		g.Edges = append(g.Edges, model.Edge{From: "a", To: "b", Kind: model.EdgeDependsOn, Status: model.StatusUnchanged})
		return g
	}

	result := Compare(build(model.StatusUnchanged), build(model.StatusRemoved))
	for _, edge := range result.Edges {
		if edge.Status != model.StatusRemoved {
			t.Fatalf("edge status = %q, want removed", edge.Status)
		}
	}
}

func edgeStatus(graph *model.Graph, from, to string) (model.Status, bool) {
	for _, edge := range graph.Edges {
		if edge.From == from && edge.To == to {
			return edge.Status, true
		}
	}
	return "", false
}

func TestChangeLines(t *testing.T) {
	oldGraph := model.New()
	oldGraph.Nodes = append(oldGraph.Nodes, model.Node{
		ID: "removed", Name: "removed", Type: model.NodeService,
		Source:   model.Source{File: "a.yaml", Line: 1},
		Metadata: map[string]string{}, Status: model.StatusUnchanged,
	})
	newGraph := model.New()
	newGraph.Nodes = append(newGraph.Nodes, model.Node{
		ID: "added", Name: "added", Type: model.NodeService,
		Source:   model.Source{File: "a.yaml", Line: 1},
		Metadata: map[string]string{}, Status: model.StatusUnchanged,
	})

	lines := ChangeLines(Compare(oldGraph, newGraph))
	if len(lines) != 2 {
		t.Fatalf("lines = %v, want 2", lines)
	}
	if !reflect.DeepEqual(lines, []string{
		"+ node added (service)",
		"- node removed (service)",
	}) {
		t.Fatalf("lines = %v", lines)
	}
}
