package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jookos/obgo/internal/couchdb"
)

// RemoteWatcher watches the CouchDB _changes feed and applies remote changes to disk.
type RemoteWatcher struct {
	db      couchdb.Client
	dataDir string
	onEvent func(ctx context.Context, event couchdb.ChangeEvent) error
	seqFile string // path to .obgo_seq file for persistence
}

// NewRemoteWatcher creates a new RemoteWatcher.
// onEvent is called for each incoming ChangeEvent; errors are logged but do not stop the watcher.
func NewRemoteWatcher(db couchdb.Client, dataDir string, onEvent func(context.Context, couchdb.ChangeEvent) error) *RemoteWatcher {
	return &RemoteWatcher{
		db:      db,
		dataDir: dataDir,
		onEvent: onEvent,
		seqFile: filepath.Join(dataDir, ".obgo_seq"),
	}
}

// Run starts the remote watcher. Blocks until ctx is cancelled or the changes feed terminates.
func (w *RemoteWatcher) Run(ctx context.Context) error {
	since := w.loadSeq()

	ch, err := w.db.Changes(ctx, since)
	if err != nil {
		return fmt.Errorf("remote watcher: open changes: %w", err)
	}

	for event := range ch {
		if err := w.onEvent(ctx, event); err != nil {
			fmt.Fprintf(os.Stderr, "remote watcher: apply event %q: %v\n", event.ID, err)
		}
		w.saveSeq(fmt.Sprint(event.Seq))
	}
	return ctx.Err()
}

// loadSeq reads the last persisted sequence number from the seq file.
// Returns "now" if no file exists, so we only watch new changes rather than
// replaying all history (which would overwrite locally-edited files).
func (w *RemoteWatcher) loadSeq() string {
	data, err := os.ReadFile(w.seqFile)
	if err != nil {
		return "now"
	}
	return string(data)
}

// saveSeq persists the current sequence number to the seq file.
func (w *RemoteWatcher) saveSeq(seq string) {
	_ = os.WriteFile(w.seqFile, []byte(seq), 0o644)
}
