package render

import (
	"testing"

	"github.com/nonomnonom/infradiff/internal/model"
)

func TestFilterFocusDepthAndHide(t *testing.T) {
	graph := model.New()
	graph.Groups = append(graph.Groups, model.Group{
		ID: "group:project:shop", Name: "shop", Kind: model.GroupProject,
		Status: model.StatusUnchanged,
	})
	graph.Nodes = append(graph.Nodes, []model.Node{
		{ID: "a", Name: "api", Type: model.NodeService, Group: "group:project:shop",
			Source: model.Source{File: "compose.yaml", Line: 1}, Metadata: map[string]string{}, Status: model.StatusUnchanged},
		{ID: "b", Name: "db", Type: model.NodeDatabase, Group: "group:project:shop",
			Source: model.Source{File: "compose.yaml", Line: 2}, Metadata: map[string]string{}, Status: model.StatusUnchanged},
		{ID: "c", Name: "cache", Type: model.NodeCache, Group: "group:project:shop",
			Source: model.Source{File: "compose.yaml", Line: 3}, Metadata: map[string]string{}, Status: model.StatusUnchanged},
		{ID: "d", Name: "unrelated", Type: model.NodeService, Group: "group:project:shop",
			Source: model.Source{File: "compose.yaml", Line: 4}, Metadata: map[string]string{}, Status: model.StatusUnchanged},
	}...)
	graph.Edges = append(graph.Edges,
		model.Edge{From: "a", To: "b", Kind: model.EdgeDependsOn, Status: model.StatusUnchanged},
		model.Edge{From: "b", To: "c", Kind: model.EdgeDependsOn, Status: model.StatusUnchanged},
	)

	filtered, err := Filter(graph, FilterOptions{Focus: "db", Depth: 1})
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if len(filtered.Nodes) != 3 {
		t.Fatalf("nodes = %+v, want api, db, and cache", filtered.Nodes)
	}
	if len(filtered.Edges) != 2 {
		t.Fatalf("edges = %+v, want both edges", filtered.Edges)
	}

	filtered, err = Filter(graph, FilterOptions{Focus: "db", Depth: 1, Hide: []string{"cache"}})
	if err != nil {
		t.Fatalf("Filter with hide: %v", err)
	}
	for _, node := range filtered.Nodes {
		if node.ID == "c" {
			t.Fatal("hidden cache node is still present")
		}
	}
}
