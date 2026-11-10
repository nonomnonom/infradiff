package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nonomnonom/infradiff/internal/model"
)

const fixtureRoot = "../../../testdata/compose"

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"well-known compose name", filepath.Join(fixtureRoot, "3-tier", "compose.yaml"), true},
		{"well-known docker-compose name", "docker-compose.yml", true},
		{"yaml with services mapping", filepath.Join(fixtureRoot, "3-tier", "compose.yaml"), true},
		{"non-yaml extension", "notes.txt", false},
		{"yaml without services", filepath.Join(fixtureRoot, "not-compose.yaml"), false},
	}

	parser := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.Detect(tt.path); got != tt.want {
				t.Fatalf("Detect(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestParseThreeTier(t *testing.T) {
	path := filepath.Join(fixtureRoot, "3-tier", "compose.yaml")
	graph, err := New().Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(graph.Groups) != 1 || graph.Groups[0].ID != "group:project:shop" {
		t.Fatalf("groups = %+v, want one shop project group", graph.Groups)
	}
	if len(graph.Nodes) != 8 {
		t.Fatalf("node count = %d, want 8", len(graph.Nodes))
	}
	if len(graph.Edges) != 10 {
		t.Fatalf("edge count = %d, want 10", len(graph.Edges))
	}

	checks := []struct {
		id       string
		nodeType model.NodeType
		line     int
		metadata map[string]string
	}{
		{"compose:service:proxy", model.NodeLoadBalancer, 4, map[string]string{"image": "nginx:1.29", "ports": "80:80"}},
		{"compose:service:web", model.NodeService, 13, map[string]string{"environment": "DATABASE_URL, SESSION_SECRET", "image": "shop/web:latest"}},
		{"compose:service:db", model.NodeDatabase, 32, map[string]string{"environment": "POSTGRES_PASSWORD, POSTGRES_USER", "image": "postgres:16"}},
		{"compose:service:cache", model.NodeCache, 42, map[string]string{"image": "redis:7"}},
		{"compose:network:frontend", model.NodeNetwork, 48, map[string]string{}},
		{"compose:volume:dbdata", model.NodeStorage, 52, map[string]string{}},
	}
	for _, want := range checks {
		node, ok := graph.Node(want.id)
		if !ok {
			t.Fatalf("missing node %q", want.id)
		}
		if node.Type != want.nodeType {
			t.Fatalf("%s type = %q, want %q", want.id, node.Type, want.nodeType)
		}
		if node.Source.Line != want.line {
			t.Fatalf("%s line = %d, want %d", want.id, node.Source.Line, want.line)
		}
		for key, value := range want.metadata {
			if node.Metadata[key] != value {
				t.Fatalf("%s metadata[%q] = %q, want %q", want.id, key, node.Metadata[key], value)
			}
		}
	}

	edgeKinds := map[string]model.EdgeKind{
		"compose:service:proxy->compose:service:web":      model.EdgeDependsOn,
		"compose:service:proxy->compose:network:frontend": model.EdgePort,
		"compose:service:web->compose:network:frontend":   model.EdgeNetwork,
		"compose:service:db->compose:volume:dbdata":       model.EdgeVolume,
		"compose:service:cache->compose:network:backend":  model.EdgeNetwork,
	}
	for key, kind := range edgeKinds {
		from, to, _ := strings.Cut(key, "->")
		if !hasEdge(graph, from, to, kind) {
			t.Fatalf("missing edge %s -> %s (%s)", from, to, kind)
		}
	}
}

func TestParseDefaultNetwork(t *testing.T) {
	graph, err := New().Parse(filepath.Join(fixtureRoot, "default-network", "compose.yaml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	network, ok := graph.Node("compose:network:default")
	if !ok {
		t.Fatal("implicit default network node is missing")
	}
	if network.Metadata["implicit"] == "" {
		t.Fatal("implicit default network should be marked in metadata")
	}
	if !hasEdge(graph, "compose:service:api", "compose:network:default", model.EdgeNetwork) {
		t.Fatal("service is not connected to the default network")
	}
}

func TestParseNeverStoresSecretValues(t *testing.T) {
	graph, err := New().Parse(filepath.Join(fixtureRoot, "secrets", "compose.yaml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	api, ok := graph.Node("compose:service:api")
	if !ok {
		t.Fatal("api node is missing")
	}
	if got := api.Metadata["environment"]; got != "API_KEY, DB_PASSWORD, LOG_LEVEL" {
		t.Fatalf("environment metadata = %q, want sorted variable names", got)
	}

	for _, node := range graph.Nodes {
		for key, value := range node.Metadata {
			for _, secret := range []string{"hunter2", "abc123"} {
				if strings.Contains(value, secret) {
					t.Fatalf("secret %q leaked into metadata %q", secret, key)
				}
			}
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{"malformed yaml", "invalid-yaml", "compose:"},
		{"unknown dependency", "unknown-dependency", `depends_on refers to undefined service "db"`},
		{"no services", "no-services", "no services defined"},
		{"undefined network", "undefined-network", `network "private" is not declared`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Parse(filepath.Join(fixtureRoot, tt.dir, "compose.yaml"))
			if err == nil {
				t.Fatalf("Parse() = nil, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() = %q, want substring %q", err.Error(), tt.want)
			}
			if !strings.Contains(err.Error(), filepath.Base(tt.dir)) {
				t.Fatalf("error %q should name the file", err.Error())
			}
		})
	}
}

func TestParseMissingFile(t *testing.T) {
	_, err := New().Parse(filepath.Join(t.TempDir(), "compose.yaml"))
	if err == nil {
		t.Fatal("Parse() = nil, want a wrapped read error")
	}
	if !strings.Contains(err.Error(), "compose:") {
		t.Fatalf("Parse() = %q, want the Compose prefix", err.Error())
	}
}

func TestNamedVolume(t *testing.T) {
	tests := []struct {
		value string
		name  string
		ok    bool
	}{
		{"dbdata:/var/lib/postgresql/data", "dbdata", true},
		{"./data:/data", "", false},
		{"/var/run/docker.sock:/var/run/docker.sock", "", false},
		{"C:\\data:/data", "", false},
		{"/data", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			name, ok := namedVolume(tt.value)
			if name != tt.name || ok != tt.ok {
				t.Fatalf("namedVolume(%q) = (%q, %v), want (%q, %v)", tt.value, name, ok, tt.name, tt.ok)
			}
		})
	}
}

func hasEdge(graph *model.Graph, from, to string, kind model.EdgeKind) bool {
	for _, edge := range graph.Edges {
		if edge.From == from && edge.To == to && edge.Kind == kind {
			return true
		}
	}
	return false
}

func TestMain(m *testing.M) {
	// Fail fast if a fixture was deleted, so tests do not silently pass.
	if _, err := os.Stat(filepath.Join(fixtureRoot, "3-tier", "compose.yaml")); err != nil {
		panic("compose fixtures are missing: " + err.Error())
	}
	os.Exit(m.Run())
}

func TestParseDirMergesOverride(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("compose.yaml", `
services:
  api:
    image: shop/api:1
    depends_on:
      - db
  db:
    image: postgres:16
`)
	write("compose.override.yaml", `
services:
  api:
    image: shop/api:2
    ports:
      - "8080:8080"
`)

	graph, err := New().ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	api, ok := graph.Node("compose:service:api")
	if !ok {
		t.Fatal("api is missing")
	}
	if api.Metadata["image"] != "shop/api:2" {
		t.Fatalf("image = %q, want override image", api.Metadata["image"])
	}
	if api.Metadata["ports"] != "8080:8080" {
		t.Fatalf("ports = %q", api.Metadata["ports"])
	}
	if api.Source.File != filepath.Join(dir, "compose.override.yaml") {
		t.Fatalf("source = %q", api.Source.File)
	}
}

func TestParseExtendsProfilesAndEnvFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write(".env", "DB_PASSWORD=super-secret\n")
	write("compose.yaml", `
services:
  base:
    image: postgres:16
    environment:
      DB_USER: shop
    env_file: .env
  app:
    extends:
      service: base
    ports:
      - "5432:5432"
  debug:
    image: shop/debug:1
    profiles: [debug]
`)

	graph, err := New().ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	app, ok := graph.Node("compose:service:app")
	if !ok {
		t.Fatalf("app is missing: %+v", graph.Nodes)
	}
	if app.Type != model.NodeDatabase || app.Metadata["image"] != "postgres:16" {
		t.Fatalf("app = %+v", app)
	}
	if !strings.Contains(app.Metadata["environment"], "DB_PASSWORD") ||
		!strings.Contains(app.Metadata["environment"], "DB_USER") {
		t.Fatalf("environment = %q", app.Metadata["environment"])
	}
	if strings.Contains(app.Metadata["environment"], "super-secret") {
		t.Fatal("env_file value leaked")
	}
	if _, ok := graph.Node("compose:service:debug"); ok {
		t.Fatal("profile service should be inactive by default")
	}
}

func TestParseExtendsExternalFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("base.yml", "services:\n  base:\n    image: postgres:16\n")
	write("compose.yaml", `
services:
  app:
    extends:
      file: base.yml
      service: base
    ports:
      - "5432:5432"
`)

	graph, err := New().ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if _, ok := graph.Node("compose:service:base"); ok {
		t.Fatal("extends-only file must not become a project service")
	}
	app, ok := graph.Node("compose:service:app")
	if !ok || app.Metadata["image"] != "postgres:16" {
		t.Fatalf("app = %+v", app)
	}
}
