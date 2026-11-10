package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nonomnonom/infradiff/internal/model"
)

func TestParseDirectory(t *testing.T) {
	graph, err := DefaultRegistry().ParsePath("../../testdata/compose/3-tier")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	if len(graph.Nodes) != 8 {
		t.Fatalf("node count = %d, want 8", len(graph.Nodes))
	}
	for _, node := range graph.Nodes {
		if node.Source.File != "compose.yaml" {
			t.Fatalf("%s source file = %q, want compose.yaml", node.ID, node.Source.File)
		}
	}
	if graph.Summary.Nodes.Unchanged != 8 {
		t.Fatalf("summary = %+v, want 8 unchanged nodes", graph.Summary)
	}
}

func TestParseKubernetesDirectory(t *testing.T) {
	graph, err := DefaultRegistry().ParsePath("../../testdata/kubernetes/shop")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	if len(graph.Nodes) != 12 || len(graph.Edges) != 10 {
		t.Fatalf("graph = %d nodes, %d edges; want 12 and 10", len(graph.Nodes), len(graph.Edges))
	}
	node, ok := graph.Node("k8s:deployment:shop/api")
	if !ok {
		t.Fatal("api deployment is missing")
	}
	if node.Source.File != "api.yaml" {
		t.Fatalf("source file = %q, want api.yaml", node.Source.File)
	}
}

func TestParseSingleFile(t *testing.T) {
	graph, err := DefaultRegistry().ParsePath("../../testdata/compose/default-network/compose.yaml")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	node, ok := graph.Node("compose:service:api")
	if !ok {
		t.Fatal("api node is missing")
	}
	if node.Source.File != "compose.yaml" {
		t.Fatalf("source file = %q, want compose.yaml", node.Source.File)
	}
}

func TestParsePathErrors(t *testing.T) {
	temp := t.TempDir()

	unknown := filepath.Join(temp, "notes.txt")
	if err := os.WriteFile(unknown, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	empty := filepath.Join(temp, "empty")
	if err := os.Mkdir(empty, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"unknown format", unknown, "unknown infrastructure format"},
		{"no supported files", empty, "no supported infrastructure files found"},
		{"missing path", filepath.Join(temp, "missing"), "cannot read"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DefaultRegistry().ParsePath(tt.path)
			if err == nil {
				t.Fatalf("ParsePath(%q) = nil, want error containing %q", tt.path, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParsePath(%q) = %q, want substring %q", tt.path, err.Error(), tt.want)
			}
		})
	}
}

func TestParsePathSkipsIgnoredDirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "node_modules", "pkg")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "services:\n  api:\n    image: shop/api:latest\n"
	if err := os.WriteFile(filepath.Join(nested, "compose.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := DefaultRegistry().ParsePath(root)
	if err == nil || !strings.Contains(err.Error(), "no supported infrastructure files") {
		t.Fatalf("ParsePath() = %v, want node_modules to be skipped", err)
	}
}

func TestMergeIdenticalDuplicates(t *testing.T) {
	graph := func() *model.Graph {
		g := model.New()
		g.Groups = append(g.Groups, model.Group{
			ID: "group:project:shop", Name: "shop", Kind: model.GroupProject, Status: model.StatusUnchanged,
		})
		g.Nodes = append(g.Nodes, model.Node{
			ID: "compose:service:api", Name: "api", Type: model.NodeService,
			Group:    "group:project:shop",
			Source:   model.Source{File: "compose.yaml", Line: 2},
			Metadata: map[string]string{"image": "shop/api"},
			Status:   model.StatusUnchanged,
		})
		return g
	}

	merged, err := Merge(graph(), graph())
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(merged.Nodes) != 1 || len(merged.Groups) != 1 {
		t.Fatalf("merged = %d nodes, %d groups; want 1 and 1", len(merged.Nodes), len(merged.Groups))
	}
}

func TestMergeConflicts(t *testing.T) {
	base := func() *model.Graph {
		g := model.New()
		g.Groups = append(g.Groups, model.Group{
			ID: "group:project:shop", Name: "shop", Kind: model.GroupProject, Status: model.StatusUnchanged,
		})
		g.Nodes = append(g.Nodes,
			model.Node{
				ID: "a", Name: "a", Type: model.NodeService, Group: "group:project:shop",
				Source: model.Source{File: "a.yaml", Line: 1}, Metadata: map[string]string{},
				Status: model.StatusUnchanged,
			},
			model.Node{
				ID: "b", Name: "b", Type: model.NodeService, Group: "group:project:shop",
				Source: model.Source{File: "a.yaml", Line: 2}, Metadata: map[string]string{},
				Status: model.StatusUnchanged,
			},
		)
		return g
	}

	tests := []struct {
		name   string
		mutate func(*model.Graph)
		want   string
	}{
		{
			name: "different node content",
			mutate: func(g *model.Graph) {
				g.Nodes[0].Name = "other"
			},
			want: `node "a" is defined twice`,
		},
		{
			name: "different group content",
			mutate: func(g *model.Graph) {
				g.Groups[0].Name = "other"
			},
			want: `group "group:project:shop" is defined twice`,
		},
		{
			name: "conflicting edge kind",
			mutate: func(g *model.Graph) {
				g.Edges[0].Kind = model.EdgeDependsOn
			},
			want: `conflicting edges "a" -> "b"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := base()
			first.Edges = append(first.Edges, model.Edge{
				From: "a", To: "b", Kind: model.EdgeNetwork, Status: model.StatusUnchanged,
			})
			second := base()
			second.Edges = append(second.Edges, model.Edge{
				From: "a", To: "b", Kind: model.EdgeNetwork, Status: model.StatusUnchanged,
			})
			tt.mutate(second)

			_, err := Merge(first, second)
			if err == nil {
				t.Fatalf("Merge() = nil, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Merge() = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestParserFor(t *testing.T) {
	composeParser, ok := DefaultRegistry().ParserFor("../../testdata/compose/3-tier/compose.yaml")
	if !ok || composeParser.Name() != "docker-compose" {
		t.Fatalf("ParserFor(compose) = (%v, %v), want docker-compose", composeParser, ok)
	}

	kubernetesParser, ok := DefaultRegistry().ParserFor("../../testdata/compose/not-compose.yaml")
	if !ok || kubernetesParser.Name() != "kubernetes" {
		t.Fatalf("ParserFor(configmap) = (%v, %v), want kubernetes", kubernetesParser, ok)
	}

	plain := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(plain, []byte("retries: 3\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if p, ok := DefaultRegistry().ParserFor(plain); ok {
		t.Fatalf("ParserFor(plain yaml) claimed by %s", p.Name())
	}
}
