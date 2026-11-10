// Package gitdiff reads repository content at a git revision without changing
// the working tree or the index.
package gitdiff

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoRoot returns the top level of the git repository containing dir.
func RepoRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git: %s is not inside a git repository", dir)
	}
	root := filepath.FromSlash(strings.TrimSpace(string(out)))
	// git may report the repository through a symlink or a Windows short
	// name. Canonicalize it so paths derived from the caller compare equal.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root, nil
}

// VerifyRef checks that ref resolves to a commit.
func VerifyRef(repoRoot, ref string) error {
	cmd := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git: revision %q not found in %s", ref, repoRoot)
	}
	return nil
}

// PathExistsAtRef reports whether path (relative to the repository root, slash
// separated) exists at ref.
func PathExistsAtRef(repoRoot, ref, path string) bool {
	if path == "." {
		return true
	}
	cmd := exec.Command("git", "-C", repoRoot, "cat-file", "-e", ref+":"+path)
	return cmd.Run() == nil
}

// ExtractRef writes the tree at ref for path into a new temporary directory and
// returns the directory plus a cleanup function. Only regular files and
// directories are extracted; symlinks and special files are skipped.
func ExtractRef(repoRoot, ref, path string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "infradiff-ref-*")
	if err != nil {
		return "", nil, fmt.Errorf("git: create temporary directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	args := []string{"-C", repoRoot, "archive", "--format=tar", ref}
	if path != "." {
		args = append(args, "--", path)
	}
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	archive, err := cmd.Output()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("git: cannot archive %s at %s: %s",
			path, ref, strings.TrimSpace(stderr.String()))
	}

	if err := extractTar(bytes.NewReader(archive), tempDir); err != nil {
		cleanup()
		return "", nil, err
	}
	return tempDir, cleanup, nil
}

func extractTar(reader io.Reader, destRoot string) error {
	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("git: read archive: %w", err)
		}

		name := filepath.FromSlash(header.Name)
		dest := filepath.Join(destRoot, name)
		rel, err := filepath.Rel(destRoot, dest)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("git: archive contains an unsafe path %q", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return fmt.Errorf("git: create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return fmt.Errorf("git: create directory: %w", err)
			}
			file, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return fmt.Errorf("git: create file: %w", err)
			}
			if _, err := io.Copy(file, archive); err != nil {
				_ = file.Close()
				return fmt.Errorf("git: write file: %w", err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("git: close file: %w", err)
			}
		default:
			// Symlinks and device files are not needed for parsing and could
			// point outside the temporary directory.
		}
	}
}
