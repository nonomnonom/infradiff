// Package watch re-runs a render callback when infrastructure files change.
package watch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Options configures Run.
type Options struct {
	// Root is the file or directory to watch.
	Root string
	// Debounce is how long to wait for quiet after an event. Defaults to 250ms.
	Debounce time.Duration
	// OnChange is called after the debounce window.
	OnChange func() error
	// OnError receives watch and OnChange errors. Optional.
	OnError func(error)
	// Ignore suppresses events for specific paths. Optional.
	Ignore func(path string) bool
	// Ready is called once every watch is installed. Optional; useful in tests.
	Ready func()
}

var ignoredDirNames = map[string]bool{
	".git":         true,
	".terraform":   true,
	".venv":        true,
	"build":        true,
	"dist":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
}

// Run watches until ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	if opts.Root == "" {
		return fmt.Errorf("watch: root path is required")
	}
	if opts.OnChange == nil {
		return fmt.Errorf("watch: OnChange callback is required")
	}
	debounce := opts.Debounce
	if debounce <= 0 {
		debounce = 250 * time.Millisecond
	}

	info, err := os.Stat(opts.Root)
	if err != nil {
		return fmt.Errorf("watch: %s: %w", opts.Root, err)
	}
	targetIsFile := !info.IsDir()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch: create watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	if targetIsFile {
		if err := watcher.Add(filepath.Dir(opts.Root)); err != nil {
			return fmt.Errorf("watch: %s: %w", opts.Root, err)
		}
	} else if err := addTree(watcher, opts.Root); err != nil {
		return err
	}
	if opts.Ready != nil {
		opts.Ready()
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	schedule := func() {
		if timer == nil {
			timer = time.NewTimer(debounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounce)
		}
		timerC = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Chmod == event.Op {
				continue
			}
			if targetIsFile && !samePath(event.Name, opts.Root) {
				continue
			}
			if opts.Ignore != nil && opts.Ignore(event.Name) {
				continue
			}
			if ignoredPath(event.Name) {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := addTree(watcher, event.Name); err != nil && opts.OnError != nil {
						opts.OnError(err)
					}
				}
			}
			schedule()
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			if opts.OnError != nil {
				opts.OnError(fmt.Errorf("watch: %w", err))
			}
		case <-timerC:
			timerC = nil
			if err := opts.OnChange(); err != nil && opts.OnError != nil {
				opts.OnError(err)
			}
		}
	}
}

func addTree(watcher *fsnotify.Watcher, root string) error {
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root && ignoredDirNames[entry.Name()] {
			return filepath.SkipDir
		}
		if err := watcher.Add(path); err != nil {
			return fmt.Errorf("watch: %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("watch: %s: %w", root, err)
	}
	return nil
}

func ignoredPath(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".infradiff-") {
		return true
	}
	return ignoredDirNames[base]
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}
