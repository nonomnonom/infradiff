package gitdiff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestExtractRef(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")

	infra := filepath.Join(repo, "infra")
	if err := os.MkdirAll(infra, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	committed := "services:\n  api:\n    image: shop/api:1\n"
	file := filepath.Join(infra, "compose.yaml")
	if err := os.WriteFile(file, []byte(committed), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "initial")

	if err := os.WriteFile(file, []byte("services:\n  api:\n    image: shop/api:2\n"), 0o600); err != nil {
		t.Fatalf("write change: %v", err)
	}

	root, err := RepoRoot(repo)
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	if filepath.Clean(root) != filepath.Clean(repo) {
		t.Fatalf("RepoRoot() = %q, want %q", root, repo)
	}
	if err := VerifyRef(root, "HEAD"); err != nil {
		t.Fatalf("VerifyRef: %v", err)
	}
	if !PathExistsAtRef(root, "HEAD", "infra/compose.yaml") {
		t.Fatal("PathExistsAtRef reported a committed file as missing")
	}
	if PathExistsAtRef(root, "HEAD", "infra/missing.yaml") {
		t.Fatal("PathExistsAtRef reported a missing file as present")
	}

	extracted, cleanup, err := ExtractRef(root, "HEAD", "infra")
	if err != nil {
		t.Fatalf("ExtractRef: %v", err)
	}
	t.Cleanup(cleanup)

	data, err := os.ReadFile(filepath.Join(extracted, "infra", "compose.yaml"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	// Git may check content out with platform line endings; parsing does not
	// care, so compare with normalized newlines.
	if got := strings.ReplaceAll(string(data), "\r\n", "\n"); got != committed {
		t.Fatalf("extracted content = %q, want %q", got, committed)
	}

	cleanup()
	if _, err := os.Stat(extracted); !os.IsNotExist(err) {
		t.Fatalf("cleanup did not remove %s", extracted)
	}
}

func TestErrors(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")

	if err := VerifyRef(repo, "does-not-exist"); err == nil ||
		!strings.Contains(err.Error(), "revision") {
		t.Fatalf("VerifyRef() = %v, want an unknown revision error", err)
	}

	nonRepo := t.TempDir()
	if _, err := RepoRoot(nonRepo); err == nil {
		t.Fatal("RepoRoot() succeeded outside a git repository")
	}
}
