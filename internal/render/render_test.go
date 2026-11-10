package render

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nonomnonom/infradiff/internal/diff"
	"github.com/nonomnonom/infradiff/internal/model"
	"github.com/nonomnonom/infradiff/internal/parser"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	composePath = "../../testdata/compose/3-tier"
	goldenDir   = "../../testdata/golden"
)

func loadCompose(t *testing.T) *model.Graph {
	t.Helper()
	graph, err := parser.DefaultRegistry().ParsePath(composePath)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return graph
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(goldenDir, 0o750); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write golden %s: %v", name, err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run UPDATE_GOLDEN=1 go test ./internal/render)", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s does not match golden\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestMermaidThreeTier(t *testing.T) {
	got := Mermaid(loadCompose(t))
	checkGolden(t, "compose-3-tier.mermaid", got)

	if again := Mermaid(loadCompose(t)); !bytes.Equal(got, again) {
		t.Fatal("Mermaid output is not deterministic")
	}
	text := string(got)
	for _, want := range []string{
		"flowchart LR",
		"subgraph g_group_project_shop",
		"classDef added",
		"classDef removed",
		"classDef changed",
		"linkStyle 0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Mermaid output is missing %q\n%s", want, text)
		}
	}
}

func TestJSONThreeTier(t *testing.T) {
	got, err := JSON(loadCompose(t))
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	checkGolden(t, "compose-3-tier.json", got)

	if again, err := JSON(loadCompose(t)); err != nil || !bytes.Equal(got, again) {
		t.Fatalf("JSON output is not deterministic (err=%v)", err)
	}

	text := string(got)
	for _, secret := range []string{"supersecret", "change-me", "hunter2"} {
		if strings.Contains(text, secret) {
			t.Fatalf("JSON output leaked secret value %q", secret)
		}
	}
	if !strings.Contains(text, `"text": "+0 added, -0 removed, ~0 changed"`) {
		t.Fatalf("JSON summary text is wrong:\n%s", text)
	}
	if strings.Contains(text, `"file": "..`) {
		t.Fatal("JSON contains a source path outside the scanned root")
	}

	validateAgainstDocumentedSchema(t, got)
}

func validateAgainstDocumentedSchema(t *testing.T, data []byte) {
	t.Helper()
	schema := documentedSchema(t)
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rendered JSON: %v", err)
	}
	if err := schema.Validate(document); err != nil {
		t.Fatalf("rendered JSON does not match docs/json-schema.md: %v", err)
	}
}

func documentedSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := os.ReadFile("../../docs/json-schema.md")
	if err != nil {
		t.Fatalf("read schema doc: %v", err)
	}
	blocks := regexp.MustCompile("(?s)```json\\s*\\n(.*?)```").FindAllStringSubmatch(string(doc), -1)
	var schemaText string
	for _, block := range blocks {
		if strings.Contains(block[1], `"$schema"`) {
			schemaText = block[1]
			break
		}
	}
	if schemaText == "" {
		t.Fatal("docs/json-schema.md has no JSON Schema block")
	}
	parsed, err := jsonschema.UnmarshalJSON(strings.NewReader(schemaText))
	if err != nil {
		t.Fatalf("parse documented schema: %v", err)
	}
	const location = "https://infradiff.dev/schema/graph-1.json"
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(location, parsed); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		t.Fatalf("compile documented schema: %v", err)
	}
	return compiled
}

func TestDiffRender(t *testing.T) {
	oldGraph, err := parser.DefaultRegistry().ParsePath("../../testdata/compose/before")
	if err != nil {
		t.Fatalf("parse before: %v", err)
	}
	newGraph, err := parser.DefaultRegistry().ParsePath("../../testdata/compose/after")
	if err != nil {
		t.Fatalf("parse after: %v", err)
	}
	result := diff.Compare(oldGraph, newGraph)

	mermaid := Mermaid(result)
	checkGolden(t, "compose-diff.mermaid", mermaid)
	text := string(mermaid)
	for _, want := range []string{
		"class n_compose_service_cache added",
		"class n_compose_service_worker removed",
		"class n_compose_service_api changed",
		"stroke:#16a34a",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("diff Mermaid is missing %q\n%s", want, text)
		}
	}

	data, err := JSON(result)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	checkGolden(t, "compose-diff.json", data)
	validateAgainstDocumentedSchema(t, data)
	if !strings.Contains(string(data), `"text": "+2 added, -2 removed, ~1 changed"`) {
		t.Fatalf("diff JSON summary is wrong:\n%s", data)
	}
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    Format
		wantErr string
	}{
		{"mermaid", FormatMermaid, ""},
		{"JSON", FormatJSON, ""},
		{"html", FormatHTML, ""},
		{" svg ", "", "unknown format"},
		{"", "", "unknown format"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseFormat(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseFormat(%q) = (%q, %v), want error %q", tt.in, got, err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("ParseFormat(%q) = (%q, %v), want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestMermaidEscapesLabels(t *testing.T) {
	g := model.New()
	g.Nodes = append(g.Nodes, model.Node{
		ID:       `weird:"id"`,
		Name:     `quote" and <tag>`,
		Type:     model.NodeService,
		Source:   model.Source{File: "x.yaml", Line: 1},
		Metadata: map[string]string{},
		Status:   model.StatusUnchanged,
	})

	out := string(Mermaid(g))
	if strings.Contains(out, `"quote"`) || strings.Contains(out, "<tag>") {
		t.Fatalf("label was not escaped:\n%s", out)
	}
	if !strings.Contains(out, "quote' and (tag)") {
		t.Fatalf("escaped label is missing:\n%s", out)
	}
}

func TestHTML(t *testing.T) {
	data, err := HTML(loadCompose(t))
	if err != nil {
		t.Fatalf("HTML: %v", err)
	}
	if len(data) < 5_000_000 {
		t.Fatalf("HTML is only %d bytes; Mermaid should be embedded", len(data))
	}
	text := string(data)
	for _, want := range []string{
		"<!doctype html>",
		`class="mermaid"`,
		"0 added, -0 removed, ~0 changed",
		"Mermaid " + mermaidVersion + " license",
		"The MIT License",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("HTML is missing %q", want)
		}
	}
	for _, forbidden := range []string{"cdn.jsdelivr", "unpkg.com", `<script src="http`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("HTML references a remote asset: %q", forbidden)
		}
	}

	again, err := HTML(loadCompose(t))
	if err != nil {
		t.Fatalf("HTML again: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Fatal("HTML output is not deterministic")
	}
}

func TestHTMLEscapesUntrustedLabels(t *testing.T) {
	g := model.New()
	g.Nodes = append(g.Nodes, model.Node{
		ID:       "x",
		Name:     `<script>alert(1)</script>`,
		Type:     model.NodeService,
		Source:   model.Source{File: "x.yaml", Line: 1},
		Metadata: map[string]string{},
		Status:   model.StatusUnchanged,
	})

	data, err := HTML(g)
	if err != nil {
		t.Fatalf("HTML: %v", err)
	}
	if strings.Contains(string(data), "<script>alert(1)</script>") {
		t.Fatal("HTML contains an unescaped script from a node name")
	}
}

func TestJSONInvalidGraphStillRenders(t *testing.T) {
	// Renderers are pure formatting; validation belongs to the parser pipeline.
	g := model.New()
	g.Nodes = append(g.Nodes, model.Node{
		ID: "x", Name: "x", Type: model.NodeOther,
		Source:   model.Source{File: "x.yaml", Line: 1},
		Metadata: map[string]string{}, Status: model.StatusAdded,
	})
	data, err := JSON(g)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
}
