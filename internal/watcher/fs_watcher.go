package watcher

import (
	"context"
	"errors"

	"github.com/fsnotify/fsnotify"
)

// LocalWatcher watches the local vault directory for changes and pushes them to CouchDB.
type LocalWatcher struct {
	dir      string
	suppress *SuppressSet
	onChange func(path string, op fsnotify.Op)
}

// NewLocalWatcher creates a new LocalWatcher.
func NewLocalWatcher(dir string, suppress *SuppressSet, onChange func(string, fsnotify.Op)) *LocalWatcher {
	return &LocalWatcher{dir: dir, suppress: suppress, onChange: onChange}
}

// Run starts the local filesystem watcher. Blocks until ctx is cancelled.
func (w *LocalWatcher) Run(ctx context.Context) error {
	return errors.New("not implemented")
}
