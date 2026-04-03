package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/jookos/obgo/internal/couchdb"
	"github.com/jookos/obgo/internal/crypto"
	"github.com/jookos/obgo/internal/watcher"
	"github.com/jookos/obgo/lib/livesync"
)

// ErrNotImplemented is returned by stub methods that are not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// Service orchestrates pull, push, and watch operations.
type Service struct {
	db           couchdb.Client
	crypto       *crypto.Service
	dataDir      string
	suppress     *watcher.SuppressSet
	OnPullFile   func(n int)
	OnPushFile   func(n int)
	OnWatchEvent func(path string, toRemote bool)
}

// New creates a new sync Service.
func New(db couchdb.Client, cr *crypto.Service, dataDir string) *Service {
	return &Service{
		db:       db,
		crypto:   cr,
		dataDir:  dataDir,
		suppress: watcher.NewSuppressSet(),
	}
}

// Watch starts the local and/or remote watcher depending on the flags.
// Blocks until ctx is cancelled.
func (s *Service) Watch(ctx context.Context, watchLocal, watchRemote bool) error {
	if !watchLocal && !watchRemote {
		return nil
	}

	remoteOnEvent := func(ctx context.Context, event couchdb.ChangeEvent) error {
		// Skip chunk documents (IDs starting with "h:") and docs without a path.
		if strings.HasPrefix(event.ID, "h:") {
			return nil
		}
		if event.Deleted {
			if event.Doc != nil && event.Doc.Path != "" {
				absPath := filepath.Join(s.dataDir, filepath.FromSlash(event.Doc.Path))
				s.suppress.Add(absPath)
				return os.Remove(absPath)
			}
			return nil
		}
		if event.Doc != nil && event.Doc.Path != "" {
			if err := s.applyRemoteDoc(ctx, *event.Doc); err != nil {
				return err
			}
			if s.OnWatchEvent != nil {
				s.OnWatchEvent(event.Doc.Path, false)
			}
		}
		return nil
	}

	localOnChange := func(path string, op fsnotify.Op) {
		if err := s.pushFile(ctx, path); err != nil {
			fmt.Fprintf(os.Stderr, "watch: push %q: %v\n", path, err)
			return
		}
		if s.OnWatchEvent != nil {
			if rel, err := filepath.Rel(s.dataDir, path); err == nil {
				s.OnWatchEvent(filepath.ToSlash(rel), true)
			}
		}
	}

	localOnRemove := func(path string) {
		relPath, err := filepath.Rel(s.dataDir, path)
		if err != nil {
			return
		}
		relPath = filepath.ToSlash(relPath)
		docID := livesync.EncodeDocID(relPath)
		existing, err := s.db.GetMeta(ctx, docID)
		if err != nil {
			return // not in CouchDB, nothing to do
		}
		existing.Deleted = true
		if _, err := s.db.PutMeta(ctx, existing); err != nil {
			fmt.Fprintf(os.Stderr, "watch: delete %q: %v\n", relPath, err)
		}
	}

	if watchLocal && watchRemote {
		rw := watcher.NewRemoteWatcher(s.db, s.dataDir, remoteOnEvent)
		lw := watcher.NewLocalWatcher(s.dataDir, s.suppress, localOnChange, localOnRemove)

		remoteErrCh := make(chan error, 1)
		localErrCh := make(chan error, 1)
		go func() { remoteErrCh <- rw.Run(ctx) }()
		go func() { localErrCh <- lw.Run(ctx) }()

		select {
		case err := <-remoteErrCh:
			return err
		case err := <-localErrCh:
			return err
		}
	}

	if watchRemote {
		rw := watcher.NewRemoteWatcher(s.db, s.dataDir, remoteOnEvent)
		return rw.Run(ctx)
	}

	// watchLocal only
	lw := watcher.NewLocalWatcher(s.dataDir, s.suppress, localOnChange, localOnRemove)
	return lw.Run(ctx)
}
