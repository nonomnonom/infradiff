package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	beforeFixture = "../../../testdata/compose/before"
	afterFixture  = "../../../testdata/compose/after"
)

type decodedGraph struct {
	Nodes []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"nodes"`
	Edges []struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Status string `json:"status"`
	} `json:"edges"`
	Summary struct {
		Text string `json:"text"`
	} `json:"summary"`
}

func TestDiffPaths(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"diff", beforeFixture, afterFixture, "-f", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Execute() = %d, stderr = %q", code, stderr.String())
	}

	for _, want := range []string{
		"+2 added, -2 removed, ~1 changed",
		"+ node compose:service:cache (cache)",
		"- node compose:service:worker (service)",
		"~ node compose:service:api (service)",
		"+ edge compose:service:cache -> compose:network:backend (network)",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want substring %q", stderr.String(), want)
		}
	}

	var graph decodedGraph
	if err := json.Unmarshal(stdout.Bytes(), &graph); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	statuses := make(map[string]string, len(graph.Nodes))
	for _, node := range graph.Nodes {
		statuses[node.ID] = node.Status
	}
	if statuses["compose:service:api"] != "changed" ||
		statuses["compose:service:cache"] != "added" ||
		statuses["compose:service:worker"] != "removed" {
		t.Fatalf("unexpected node statuses: %+v", statuses)
	}
}

func TestDiffFailOnChange(t *testing.T) {
	t.Run("changes", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Execute([]string{"diff", beforeFixture, afterFixture, "--fail-on-change", "-f", "json"}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("Execute() = %d, want 1", code)
		}
		if strings.Contains(stderr.String(), "infradiff:") {
			t.Fatalf("stderr should not print an error for a normal change: %q", stderr.String())
		}
		if !strings.Contains(stderr.String(), "+2 added") {
			t.Fatalf("stderr = %q, want a summary", stderr.String())
		}
	})
	t.Run("no changes", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Execute([]string{"diff", beforeFixture, beforeFixture, "--fail-on-change", "-f", "json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("Execute() = %d, want 0", code)
		}
	})
}

func TestDiffErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no paths", []string{"diff"}, "needs two paths"},
		{"one path", []string{"diff", beforeFixture}, "needs two paths"},
		{"git with two paths", []string{"diff", "--git", "HEAD", "a", "b"}, "at most one path"},
		{"missing path", []string{"diff", "does-not-exist", afterFixture}, "cannot read"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Execute(tt.args, &stdout, &stderr); code != 1 {
				t.Fatalf("Execute(%v) = %d, want 1", tt.args, code)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), tt.want)
			}
		})
	}
}

func TestDiffGitRevision(t *testing.T) {
	repo := t.TempDir()
	infra := filepath.Join(repo, "infra")
	if err := os.MkdirAll(infra, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	before, err := os.ReadFile(filepath.Join(beforeFixture, "compose.yaml"))
	if err != nil {
		t.Fatalf("read before fixture: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(afterFixture, "compose.yaml"))
	if err != nil {
		t.Fatalf("read after fixture: %v", err)
	}
	file := filepath.Join(infra, "compose.yaml")
	if err := os.WriteFile(file, before, 0o600); err != nil {
		t.Fatalf("write before: %v", err)
	}

	runGitCommand(t, repo, "init", "-q")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")
	runGitCommand(t, repo, "config", "user.name", "Test")
	runGitCommand(t, repo, "add", ".")
	runGitCommand(t, repo, "commit", "-q", "-m", "initial")

	if err := os.WriteFile(file, after, 0o600); err != nil {
		t.Fatalf("write after: %v", err)
	}

	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"diff", "--git", "HEAD", "infra", "-f", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Execute() = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "+2 added, -2 removed, ~1 changed") {
		t.Fatalf("stderr = %q, want the expected summary", stderr.String())
	}

	var graph decodedGraph
	if err := json.Unmarshal(stdout.Bytes(), &graph); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	statuses := make(map[string]string, len(graph.Nodes))
	for _, node := range graph.Nodes {
		statuses[node.ID] = node.Status
	}
	if statuses["compose:service:cache"] != "added" || statuses["compose:service:worker"] != "removed" {
		t.Fatalf("unexpected node statuses: %+v", statuses)
	}
}

func TestDiffGitUnknownRevision(t *testing.T) {
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-q")
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"diff", "--git", "nope"}, &stdout, &stderr); code != 1 {
		t.Fatalf("Execute() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "revision") {
		t.Fatalf("stderr = %q, want an unknown revision error", stderr.String())
	}
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDiffFailOnGroupChange(t *testing.T) {
	before := t.TempDir()
	after := t.TempDir()
	resource := "resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}\n"
	if err := os.WriteFile(filepath.Join(before, "main.tf"), []byte(resource), 0o600); err != nil {
		t.Fatalf("write before: %v", err)
	}
	if err := os.WriteFile(filepath.Join(after, "main.tf"), []byte(resource+"\nmodule \"empty\" {\n  source = \"./modules/empty\"\n}\n"), 0o600); err != nil {
		t.Fatalf("write after: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(after, "modules", "empty"), 0o750); err != nil {
		t.Fatalf("mkdir module: %v", err)
	}
	if err := os.WriteFile(filepath.Join(after, "modules", "empty", "main.tf"), nil, 0o600); err != nil {
		t.Fatalf("write empty module: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"diff", before, after, "--fail-on-change", "-f", "json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("Execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "+1 added") ||
		!strings.Contains(stderr.String(), "+ group group:module:empty") {
		t.Fatalf("stderr = %q, want group change", stderr.String())
	}
}
