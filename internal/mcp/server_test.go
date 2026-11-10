package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nonomnonom/infradiff/internal/model"
)

var testBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "infradiff-mcp-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testBinary = filepath.Join(dir, "infradiff")
	if runtime.GOOS == "windows" {
		testBinary += ".exe"
	}
	build := exec.Command("go", "build", "-o", testBinary, "../../cmd/infradiff")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "building infradiff: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
}

type client struct {
	t       *testing.T
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  *bytes.Buffer
	nextID  int
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repository root: %v", err)
	}
	return root
}

func startClient(t *testing.T, workspace string) *client {
	t.Helper()
	return startClientArgs(t, workspace, "mcp")
}

func startClientArgs(t *testing.T, dir string, args ...string) *client {
	t.Helper()
	c := &client{t: t, stderr: &bytes.Buffer{}}
	c.cmd = exec.Command(testBinary, args...)
	c.cmd.Dir = dir
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	c.cmd.Stderr = c.stderr
	c.stdin = stdin
	c.scanner = bufio.NewScanner(stdout)
	c.scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	if err := c.cmd.Start(); err != nil {
		t.Fatalf("starting server: %v", err)
	}
	t.Cleanup(func() {
		_ = c.stdin.Close()
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = c.cmd.Process.Kill()
			<-done
		}
		if t.Failed() && c.stderr.Len() > 0 {
			t.Logf("server stderr: %s", c.stderr.String())
		}
	})

	resp := c.call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "infradiff-test", "version": "1"},
	})
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &initialized); err != nil {
		t.Fatalf("decoding initialize result: %v", err)
	}
	if initialized.ServerInfo.Name != "infradiff" {
		t.Fatalf("serverInfo.name = %q, want infradiff", initialized.ServerInfo.Name)
	}
	if initialized.ProtocolVersion == "" {
		t.Fatal("initialize result is missing protocolVersion")
	}
	c.notify("notifications/initialized")
	return c
}

func (c *client) send(message any) {
	c.t.Helper()
	data, err := json.Marshal(message)
	if err != nil {
		c.t.Fatalf("encoding request: %v", err)
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		c.t.Fatalf("writing request: %v", err)
	}
}

func (c *client) notify(method string) {
	c.send(map[string]any{"jsonrpc": "2.0", "method": method})
}

func (c *client) call(method string, params any) rpcResponse {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	c.send(request)

	for c.scanner.Scan() {
		line := append([]byte(nil), c.scanner.Bytes()...)
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil {
			c.t.Fatalf("decoding response to %s: %v (line: %s)", method, err, line)
		}
		if response.ID == nil || *response.ID != id {
			continue
		}
		if response.Error != nil {
			c.t.Fatalf("%s failed: %d %s", method, response.Error.Code, response.Error.Message)
		}
		return response
	}
	c.t.Fatalf("server closed stdout before answering %s (stderr: %s)", method, c.stderr.String())
	return rpcResponse{}
}

func (c *client) callTool(name string, arguments any) toolResult {
	c.t.Helper()
	resp := c.call("tools/call", map[string]any{"name": name, "arguments": arguments})
	var result toolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		c.t.Fatalf("decoding %s result: %v", name, err)
	}
	return result
}

func toolText(t *testing.T, result toolResult) string {
	t.Helper()
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("expected one text content item, got %#v", result.Content)
	}
	return result.Content[0].Text
}

func TestToolsList(t *testing.T) {
	c := startClient(t, repoRoot(t))
	resp := c.call("tools/list", nil)
	var listed struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &listed); err != nil {
		t.Fatalf("decoding tools/list: %v", err)
	}
	want := map[string]bool{"generate_diagram": false, "diff_infrastructure": false, "explain_graph": false}
	if len(listed.Tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(listed.Tools), len(want))
	}
	for _, tool := range listed.Tools {
		if _, ok := want[tool.Name]; !ok {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		want[tool.Name] = true
		if tool.InputSchema["type"] != "object" {
			t.Errorf("%s input schema type = %v, want object", tool.Name, tool.InputSchema["type"])
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestGenerateDiagramMatchesGolden(t *testing.T) {
	c := startClient(t, repoRoot(t))
	result := c.callTool("generate_diagram", map[string]any{
		"path":   "testdata/compose/3-tier",
		"format": "json",
	})
	if result.IsError {
		t.Fatalf("generate_diagram failed: %s", toolText(t, result))
	}
	golden, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "golden", "compose-3-tier.json"))
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if got := toolText(t, result); got != string(golden) {
		t.Fatalf("generate_diagram output differs from golden\n--- got ---\n%s\n--- want ---\n%s", got, golden)
	}
}

func TestDiffInfrastructureMatchesGolden(t *testing.T) {
	c := startClient(t, repoRoot(t))
	result := c.callTool("diff_infrastructure", map[string]any{
		"old_path": "testdata/compose/before",
		"new_path": "testdata/compose/after",
	})
	if result.IsError {
		t.Fatalf("diff_infrastructure failed: %s", toolText(t, result))
	}
	golden, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "golden", "compose-diff.mermaid"))
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if got := toolText(t, result); got != string(golden) {
		t.Fatalf("diff_infrastructure output differs from golden\n--- got ---\n%s\n--- want ---\n%s", got, golden)
	}
}

func TestExplainGraphReportsRisks(t *testing.T) {
	c := startClient(t, repoRoot(t))
	result := c.callTool("explain_graph", map[string]any{"path": "testdata/compose/risky"})
	if result.IsError {
		t.Fatalf("explain_graph failed: %s", toolText(t, result))
	}
	if len(result.StructuredContent) == 0 {
		t.Fatal("explain_graph did not return structuredContent")
	}
	raw := result.StructuredContent
	var explain Explain
	if err := json.Unmarshal(raw, &explain); err != nil {
		t.Fatalf("decoding explain result: %v", err)
	}
	if explain.NodeCount != 2 || explain.EdgeCount != 1 {
		t.Fatalf("counts = %d nodes, %d edges; want 2 and 1", explain.NodeCount, explain.EdgeCount)
	}
	if explain.NodesByType[string(model.NodeDatabase)] != 1 {
		t.Errorf("nodes_by_type = %v, want one database", explain.NodesByType)
	}
	var highPorts, unpinned bool
	for _, risk := range explain.Risks {
		if risk.Node != "compose:service:db" {
			continue
		}
		if risk.Severity == "high" && strings.Contains(risk.Message, "publishes host ports") {
			highPorts = true
		}
		if risk.Severity == "medium" && strings.Contains(risk.Message, "unpinned image tag") {
			unpinned = true
		}
	}
	if !highPorts {
		t.Errorf("missing high-severity published-port risk: %#v", explain.Risks)
	}
	if !unpinned {
		t.Errorf("missing unpinned-image risk: %#v", explain.Risks)
	}
}

func TestGenerateRedactsSecrets(t *testing.T) {
	workspace := t.TempDir()
	compose := "services:\n  db:\n    image: postgres:16\n    environment:\n      POSTGRES_PASSWORD: hunter2\n"
	if err := os.WriteFile(filepath.Join(workspace, "compose.yaml"), []byte(compose), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	c := startClient(t, workspace)
	result := c.callTool("generate_diagram", map[string]any{"path": ".", "format": "json"})
	if result.IsError {
		t.Fatalf("generate_diagram failed: %s", toolText(t, result))
	}
	text := toolText(t, result)
	if strings.Contains(text, "hunter2") {
		t.Fatalf("secret value leaked into MCP output: %s", text)
	}
	if !strings.Contains(text, "postgres:16") {
		t.Fatalf("image missing from output: %s", text)
	}
}

func TestToolRejectsPathsOutsideWorkspace(t *testing.T) {
	c := startClient(t, repoRoot(t))
	for _, path := range []string{"../go.mod", filepath.Join(repoRoot(t), "..", "go.mod")} {
		result := c.callTool("generate_diagram", map[string]any{"path": path})
		if !result.IsError {
			t.Fatalf("path %q was accepted, want an error", path)
		}
		if text := toolText(t, result); !strings.Contains(text, "outside the workspace") {
			t.Errorf("path %q error = %q, want an outside-the-workspace message", path, text)
		}
	}
}

func TestMCPCommandAcceptsWorkspaceArgument(t *testing.T) {
	c := startClientArgs(t, t.TempDir(), "mcp", repoRoot(t))
	result := c.callTool("generate_diagram", map[string]any{
		"path":   "testdata/compose/3-tier",
		"format": "mermaid",
	})
	if result.IsError {
		t.Fatalf("generate_diagram with an explicit workspace failed: %s", toolText(t, result))
	}
	if text := toolText(t, result); !strings.Contains(text, "flowchart") {
		t.Fatalf("output is not a Mermaid flowchart: %s", text)
	}
}

func TestToolRejectsUnknownFormat(t *testing.T) {
	c := startClient(t, repoRoot(t))
	result := c.callTool("generate_diagram", map[string]any{
		"path":   "testdata/compose/3-tier",
		"format": "svg",
	})
	if !result.IsError {
		t.Fatal("format svg was accepted, want an error")
	}
	if text := toolText(t, result); !strings.Contains(text, `unknown format "svg"`) {
		t.Errorf("error = %q, want unknown format message", text)
	}
}

func TestResolveMissingPathExplainsBrokenLink(t *testing.T) {
	workspace := Workspace{Root: t.TempDir()}
	_, err := workspace.resolve("missing.yaml")
	if err == nil {
		t.Fatal("resolve() = nil, want an error")
	}
	if !strings.Contains(err.Error(), "cannot be resolved inside the workspace") ||
		!strings.Contains(err.Error(), "broken symbolic link") {
		t.Fatalf("resolve() = %q", err)
	}
}
