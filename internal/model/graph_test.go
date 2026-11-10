package model

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const fixturePath = "../../testdata/model/graph.json"

func loadFixture(t *testing.T) *Graph {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var g Graph
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return &g
}

func TestNew(t *testing.T) {
	g := New()
	if g.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", g.SchemaVersion, SchemaVersion)
	}
	if g.Nodes == nil || g.Edges == nil || g.Groups == nil {
		t.Fatal("New must initialize all slices so JSON renders as [] instead of null")
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("empty graph should validate, got: %v", err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Graph)
		want   string
	}{
		{
			name:   "fixture is valid",
			mutate: func(*Graph) {},
		},
		{
			name:   "wrong schema version",
			mutate: func(g *Graph) { g.SchemaVersion = 2 },
			want:   "schema_version is 2",
		},
		{
			name:   "duplicate node id",
			mutate: func(g *Graph) { g.Nodes = append(g.Nodes, g.Nodes[0]) },
			want:   `duplicate node id "compose:service:api"`,
		},
		{
			name:   "unsupported node type",
			mutate: func(g *Graph) { g.Nodes[0].Type = "server" },
			want:   `type "server" is not a supported node type`,
		},
		{
			name:   "node without name",
			mutate: func(g *Graph) { g.Nodes[0].Name = "" },
			want:   `node "compose:service:api": name is required`,
		},
		{
			name:   "node without source file",
			mutate: func(g *Graph) { g.Nodes[0].Source.File = "" },
			want:   "source.file is required",
		},
		{
			name:   "nil metadata",
			mutate: func(g *Graph) { g.Nodes[0].Metadata = nil },
			want:   "metadata is null",
		},
		{
			name:   "undefined group",
			mutate: func(g *Graph) { g.Nodes[0].Group = "group:project:other" },
			want:   `group "group:project:other" is not defined`,
		},
		{
			name:   "bad node status",
			mutate: func(g *Graph) { g.Nodes[0].Status = "modified" },
			want:   `status "modified" is not one of`,
		},
		{
			name:   "edge references missing node",
			mutate: func(g *Graph) { g.Edges[0].From = "compose:service:missing" },
			want:   `from node "compose:service:missing" is not defined`,
		},
		{
			name:   "self edge",
			mutate: func(g *Graph) { g.Edges[0].To = g.Edges[0].From },
			want:   "a node cannot point at itself",
		},
		{
			name:   "duplicate edge",
			mutate: func(g *Graph) { g.Edges = append(g.Edges, g.Edges[0]) },
			want:   "duplicate edge",
		},
		{
			name:   "bad edge kind",
			mutate: func(g *Graph) { g.Edges[0].Kind = "uses" },
			want:   `kind "uses" is not a supported edge kind`,
		},
		{
			name:   "bad edge status",
			mutate: func(g *Graph) { g.Edges[0].Status = "stale" },
			want:   `status "stale" is not one of`,
		},
		{
			name:   "duplicate group id",
			mutate: func(g *Graph) { g.Groups = append(g.Groups, g.Groups[0]) },
			want:   `duplicate group id "group:project:shop"`,
		},
		{
			name:   "bad group kind",
			mutate: func(g *Graph) { g.Groups[0].Kind = "team" },
			want:   `group kind "team" is not one of`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := loadFixture(t)
			tt.mutate(g)
			err := g.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestSort(t *testing.T) {
	g := loadFixture(t)
	g.Sort()

	wantNodes := []string{
		"compose:network:default",
		"compose:service:api",
		"compose:service:db",
	}
	var gotNodes []string
	for _, n := range g.Nodes {
		gotNodes = append(gotNodes, n.ID)
	}
	if !reflect.DeepEqual(gotNodes, wantNodes) {
		t.Fatalf("node order = %v, want %v", gotNodes, wantNodes)
	}

	wantEdges := [][2]string{
		{"compose:service:api", "compose:service:db"},
		{"compose:service:db", "compose:network:default"},
	}
	var gotEdges [][2]string
	for _, e := range g.Edges {
		gotEdges = append(gotEdges, [2]string{e.From, e.To})
	}
	if !reflect.DeepEqual(gotEdges, wantEdges) {
		t.Fatalf("edge order = %v, want %v", gotEdges, wantEdges)
	}

	sortedOnce := *g
	g.Sort()
	if !reflect.DeepEqual(*g, sortedOnce) {
		t.Fatal("Sort must be idempotent")
	}
}

func TestNodeLookup(t *testing.T) {
	g := loadFixture(t)

	node, ok := g.Node("compose:service:db")
	if !ok {
		t.Fatal("Node(compose:service:db) not found")
	}
	if node.Name != "db" || node.Type != NodeDatabase {
		t.Fatalf("unexpected node: %+v", node)
	}

	if _, ok := g.Node("compose:service:missing"); ok {
		t.Fatal("Node() reported a missing node as found")
	}
}

func TestJSONRoundTripIsStable(t *testing.T) {
	g := loadFixture(t)
	g.Sort()

	first, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("marshal again: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("marshalling the same graph twice produced different bytes")
	}

	var back Graph
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := back.Validate(); err != nil {
		t.Fatalf("round-tripped graph does not validate: %v", err)
	}
}
