package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const composeFixture = "../../../testdata/compose/3-tier"

func TestGenerateJSONToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"generate", composeFixture, "-f", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Execute() = %d, stderr = %q", code, stderr.String())
	}

	var graph struct {
		SchemaVersion int `json:"schema_version"`
		Nodes         []struct {
			ID string `json:"id"`
		} `json:"nodes"`
		Summary struct {
			Text string `json:"text"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &graph); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if graph.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", graph.SchemaVersion)
	}
	if len(graph.Nodes) != 8 {
		t.Fatalf("node count = %d, want 8", len(graph.Nodes))
	}
	if graph.Summary.Text != "+0 added, -0 removed, ~0 changed" {
		t.Fatalf("summary text = %q", graph.Summary.Text)
	}
}

func TestGenerateMermaidToFile(t *testing.T) {
	output := filepath.Join(t.TempDir(), "graph.mermaid")

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"generate", composeFixture, "-f", "mermaid", "-o", output}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Execute() = %d, stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty when -o is used", stdout.String())
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "flowchart LR") {
		t.Fatalf("output file does not look like Mermaid:\n%s", data)
	}
}

func TestGenerateErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"invalid format", []string{"generate", composeFixture, "-f", "html"}, `unknown format "html"`},
		{"missing path", []string{"generate"}, "accepts 1 arg"},
		{"unknown path", []string{"generate", "does-not-exist"}, "cannot read"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Execute(tt.args, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("Execute(%v) = %d, want 1", tt.args, code)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), tt.want)
			}
		})
	}
}
