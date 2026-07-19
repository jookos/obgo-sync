package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// A directory moved into the vault arrives already populated, and the kernel
// reports only the directory itself — there are no per-file create events for
// the contents. Without an explicit walk those files are never surfaced, so
// they are never pushed and the moved content silently never reaches CouchDB.
func TestLocalWatcher_MovedDirectoryTreePushesExistingContents(t *testing.T) {
	dir := t.TempDir()
	suppress := NewSuppressSet()

	var mu sync.Mutex
	var gotPaths []string

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lw := NewLocalWatcher(dir, suppress, func(path string, op fsnotify.Op) {
		mu.Lock()
		gotPaths = append(gotPaths, path)
		mu.Unlock()
	}, nil)

	errCh := make(chan error, 1)
	go func() { errCh <- lw.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)

	// Build a populated tree outside the vault, then move it in wholesale.
	staging := t.TempDir()
	sourceTree := filepath.Join(staging, "Lit")
	if err := os.MkdirAll(filepath.Join(sourceTree, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{
		"a.md":        "alpha",
		"nested/b.md": "bravo",
		".hidden.md":  "must be skipped",
	} {
		if err := os.WriteFile(filepath.Join(sourceTree, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	moved := filepath.Join(dir, "Lit")
	if err := os.Rename(sourceTree, moved); err != nil {
		t.Fatal(err)
	}

	wantA := filepath.Join(moved, "a.md")
	wantB := filepath.Join(moved, "nested", "b.md")
	hidden := filepath.Join(moved, ".hidden.md")

	seen := func(p string) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, g := range gotPaths {
			if g == p {
				return true
			}
		}
		return false
	}

	// No file is written after the move: the contents must be surfaced purely
	// from the directory-create event.
	if !waitForEvent(t, 2*time.Second, func() bool { return seen(wantA) && seen(wantB) }) {
		mu.Lock()
		snapshot := append([]string(nil), gotPaths...)
		mu.Unlock()
		t.Errorf("expected onChange for pre-existing files in the moved tree (%q and %q), got: %v",
			wantA, wantB, snapshot)
	}

	if seen(hidden) {
		t.Errorf("hidden file %q must not be surfaced (handleEvent skips dotted paths)", hidden)
	}

	cancel()
	_ = errCh
}
