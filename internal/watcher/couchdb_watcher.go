package watcher

import (
	"context"
	"errors"

	"github.com/jookos/obgo/internal/couchdb"
)

// RemoteWatcher watches the CouchDB _changes feed and applies remote changes to disk.
type RemoteWatcher struct {
	db      couchdb.Client
	onEvent func(couchdb.ChangeEvent)
	lastSeq string
}

// NewRemoteWatcher creates a new RemoteWatcher.
func NewRemoteWatcher(db couchdb.Client, onEvent func(couchdb.ChangeEvent)) *RemoteWatcher {
	return &RemoteWatcher{db: db, onEvent: onEvent}
}

// Run starts the remote watcher. Blocks until ctx is cancelled.
func (w *RemoteWatcher) Run(ctx context.Context) error {
	return errors.New("not implemented")
}
