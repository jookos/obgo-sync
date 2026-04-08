package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/jookos/obgo-sync/internal/couchdb"
	"github.com/jookos/obgo-sync/internal/crypto"
	"github.com/jookos/obgo-sync/internal/watcher"
	"github.com/jookos/obgo-sync/lib/livesync"
)

// ErrNotImplemented is returned by stub methods that are not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// Service orchestrates pull, push, and watch operations.
type Service struct {
	db           couchdb.Client
	crypto       *crypto.Service
	dataDir      string
	suppress     *watcher.SuppressSet
	OnPullFile        func(n int)
	OnPushFile        func(n int)
	OnWatchEvent      func(path string, toRemote bool)
	OnDeleteFile      func(path string)             // called when a local file is removed (pull or watch)
	OnTombstone       func(path string)             // called when a remote doc is tombstoned (push or watch)
	OnRawChangeEvent  func(event couchdb.ChangeEvent) // called for every raw _changes event (debug)
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

// List returns the meta documents in the remote vault, optionally filtered by a
// vault-relative path prefix. prefix="" returns all documents; prefix ending
// with "/" returns documents inside that folder; a bare filename returns only
// that file (exact path match). Results are sorted by path.
func (s *Service) List(ctx context.Context, prefix string) ([]couchdb.MetaDoc, error) {
	docs, err := s.db.AllMetaDocs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	var result []couchdb.MetaDoc
	for _, doc := range docs {
		if doc.Deleted {
			continue
		}
		if prefix != "" && !strings.HasPrefix(doc.Path, prefix) {
			continue
		}
		result = append(result, doc)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

// Watch starts the local and/or remote watcher depending on the flags.
// Blocks until ctx is cancelled.
func (s *Service) Watch(ctx context.Context, watchLocal, watchRemote bool) error {
	if !watchLocal && !watchRemote {
		return nil
	}

	remoteOnEvent := func(ctx context.Context, event couchdb.ChangeEvent) error {
		if s.OnRawChangeEvent != nil {
			s.OnRawChangeEvent(event)
		}
		// Skip non-file documents (chunk IDs h:..., index IDs i:/f:/ix:..., etc.)
		_, isFile := livesync.DecodeDocID(event.ID)
		if !isFile {
			return nil
		}

		// Resolve the path: prefer the doc body field, fall back to decoding the event ID.
		path := ""
		if event.Doc != nil {
			path = event.Doc.Path
		}
		if path == "" {
			path, _ = livesync.DecodeDocID(event.ID)
		}
		if path == "" {
			return nil
		}

		if event.Deleted {
			absPath := resolveCase(s.dataDir, path)
			s.suppress.Add(absPath)
			_ = os.Remove(absPath)
			if s.OnDeleteFile != nil {
				s.OnDeleteFile(path)
			}
			return nil
		}
		if event.Doc != nil {
			event.Doc.Path = path
			resolved, rerr := s.resolveConflicts(ctx, *event.Doc)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "watch: resolve conflicts %q: %v\n", path, rerr)
				resolved = *event.Doc
			}
			if err := s.applyRemoteDoc(ctx, resolved); err != nil {
				return err
			}
			if s.OnWatchEvent != nil {
				s.OnWatchEvent(path, false)
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
			return
		}
		if s.OnTombstone != nil {
			s.OnTombstone(relPath)
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
