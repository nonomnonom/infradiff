package watch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRunDebouncesChanges(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	calls := 0
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Root:     dir,
			Debounce: 80 * time.Millisecond,
			Ready:    func() { close(ready) },
			OnChange: func() error {
				mu.Lock()
				calls++
				mu.Unlock()
				return nil
			},
		})
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not become ready")
	}

	for i := 0; i < 3; i++ {
		if err := os.WriteFile(file, []byte("services:\n  api:\n    image: shop/api:1\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := calls
		mu.Unlock()
		if count > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	count := calls
	mu.Unlock()
	if count == 0 {
		t.Fatal("OnChange was never called")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil after cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestRunHonorsIgnore(t *testing.T) {
	dir := t.TempDir()
	ignored := filepath.Join(dir, "ignored.txt")
	if err := os.WriteFile(ignored, []byte("one"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	called := make(chan struct{}, 1)
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Root:     dir,
			Debounce: 30 * time.Millisecond,
			Ready:    func() { close(ready) },
			Ignore:   func(path string) bool { return samePath(path, ignored) },
			OnChange: func() error {
				select {
				case called <- struct{}{}:
				default:
				}
				return nil
			},
		})
	}()
	<-ready

	if err := os.WriteFile(ignored, []byte("two"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-called:
		t.Fatal("OnChange fired for an ignored path")
	case <-time.After(400 * time.Millisecond):
	}

	cancel()
	<-done
}

func TestRunReportsChangeErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{})
	errs := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Root:     dir,
			Debounce: 40 * time.Millisecond,
			Ready:    func() { close(ready) },
			OnChange: func() error { return errors.New("render failed") },
			OnError: func(err error) {
				select {
				case errs <- err:
				default:
				}
			},
		})
	}()
	<-ready

	if err := os.WriteFile(file, []byte("services:\n  api:\n    image: shop/api:1\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case err := <-errs:
		if err == nil || err.Error() != "render failed" {
			t.Fatalf("OnError(%v), want render failed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnError was never called")
	}

	cancel()
	<-done
}

func TestRunRequiresOptions(t *testing.T) {
	if err := Run(context.Background(), Options{}); err == nil {
		t.Fatal("Run() with no root succeeded")
	}
	dir := t.TempDir()
	if err := Run(context.Background(), Options{Root: dir}); err == nil {
		t.Fatal("Run() without OnChange succeeded")
	}
}

func TestIgnoredPath(t *testing.T) {
	for _, path := range []string{".infradiff-123", "C:/repo/.git", "/repo/node_modules"} {
		if !ignoredPath(path) {
			t.Fatalf("ignoredPath(%q) = false", path)
		}
	}
	if ignoredPath("compose.yaml") {
		t.Fatal("ignoredPath(compose.yaml) = true")
	}
}
