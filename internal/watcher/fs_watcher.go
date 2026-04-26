package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultDebounceDur = 500 * time.Millisecond

type LocalWatcherOption func(*LocalWatcher)

func WithDebounce(d time.Duration) LocalWatcherOption {
	return func(w *LocalWatcher) { w.debounceDur = d }
}

// LocalWatcher watches the local vault directory for changes and pushes them to CouchDB.
type LocalWatcher struct {
	dir            string
	suppress       *SuppressSet
	onChange       func(path string, op fsnotify.Op)
	onRemove       func(path string)
	debounceDur    time.Duration
	pendingRemoves map[string]*time.Timer
	// removeFired receives paths from expired debounce timers. All map access
	// stays in the Run goroutine; timer goroutines only do a channel send.
	removeFired chan string
}

// NewLocalWatcher creates a new LocalWatcher.
func NewLocalWatcher(dir string, suppress *SuppressSet, onChange func(string, fsnotify.Op), onRemove func(string), opts ...LocalWatcherOption) *LocalWatcher {
	w := &LocalWatcher{
		dir:            dir,
		suppress:       suppress,
		onChange:       onChange,
		onRemove:       onRemove,
		debounceDur:    defaultDebounceDur,
		pendingRemoves: make(map[string]*time.Timer),
		removeFired:    make(chan string, 32),
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Run starts the local filesystem watcher. Blocks until ctx is cancelled.
func (w *LocalWatcher) Run(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fs watcher: create: %w", err)
	}
	defer fw.Close()

	// Add root dir and all subdirectories recursively.
	if err := w.addDirRecursive(fw, w.dir); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			w.cancelAllPendingRemoves()
			return ctx.Err()
		case event, ok := <-fw.Events:
			if !ok {
				return nil
			}
			w.handleEvent(fw, event)
		case path := <-w.removeFired:
			delete(w.pendingRemoves, path)
			if w.onRemove != nil && !w.suppress.IsSuppressed(path) {
				w.onRemove(path)
			}
		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "fs watcher: %v\n", err)
		}
	}
}

func (w *LocalWatcher) addDirRecursive(fw *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable paths
		}
		if d.IsDir() {
			// Skip hidden dirs (e.g. .git)
			if d.Name() != "." && len(d.Name()) > 0 && d.Name()[0] == '.' {
				return filepath.SkipDir
			}
			return fw.Add(path)
		}
		return nil
	})
}

func (w *LocalWatcher) handleEvent(fw *fsnotify.Watcher, event fsnotify.Event) {
	base := filepath.Base(event.Name)
	if len(base) > 0 && base[0] == '.' {
		return
	}

	switch {
	case event.Has(fsnotify.Create):
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			_ = fw.Add(event.Name)
			return
		}
		w.cancelPendingRemove(event.Name)
		fallthrough
	case event.Has(fsnotify.Write):
		w.cancelPendingRemove(event.Name)
		if w.suppress.IsSuppressed(event.Name) {
			return
		}
		if w.onChange != nil {
			w.onChange(event.Name, event.Op)
		}
	case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
		if w.suppress.IsSuppressed(event.Name) {
			return
		}
		if t, ok := w.pendingRemoves[event.Name]; ok {
			t.Stop()
		}
		path := event.Name
		w.pendingRemoves[path] = time.AfterFunc(w.debounceDur, func() {
			select {
			case w.removeFired <- path:
			default:
			}
		})
	}
}

func (w *LocalWatcher) cancelPendingRemove(path string) {
	if t, ok := w.pendingRemoves[path]; ok {
		t.Stop()
		delete(w.pendingRemoves, path)
	}
}

func (w *LocalWatcher) cancelAllPendingRemoves() {
	for path, t := range w.pendingRemoves {
		t.Stop()
		delete(w.pendingRemoves, path)
	}
}
