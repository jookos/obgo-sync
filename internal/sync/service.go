package sync

import (
	"context"
	"encoding/base64"
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
	db               couchdb.Client
	crypto           *crypto.Service
	dataDir          string
	suppress         *watcher.SuppressSet
	obfuscated       bool   // true when the remote vault uses path obfuscation
	obfuscationMode  string // "auto" | "on" | "off"; set by SetObfuscationMode
	syncHidden       bool
	OnPullFile       func(n int)
	OnPushFile       func(n int)
	OnWatchEvent     func(path string, toRemote bool)
	OnDeleteFile     func(path string)
	OnTombstone      func(path string)
	OnRawChangeEvent func(event couchdb.ChangeEvent)
}

// New creates a new sync Service.
func New(db couchdb.Client, cr *crypto.Service, dataDir string) *Service {
	return &Service{
		db:              db,
		crypto:          cr,
		dataDir:         dataDir,
		suppress:        watcher.NewSuppressSet(),
		obfuscationMode: "auto",
	}
}

// SetObfuscationMode sets the path-obfuscation mode ("auto", "on", or "off").
// "on" immediately marks the vault as obfuscated. "off" disables auto-detection.
func (s *Service) SetObfuscationMode(mode string) {
	s.obfuscationMode = mode
	if mode == "on" {
		s.obfuscated = true
	}
}

// SetSyncHidden controls whether local watch mode observes dotfiles and
// dot-directories. Internal .obgo* paths and .git remain ignored.
func (s *Service) SetSyncHidden(syncHidden bool) {
	s.syncHidden = syncHidden
}

// loadSalt fetches the PBKDF2 salt from _local/obsidian_livesync_sync_parameters
// and configures the crypto service. No-op if E2EE is disabled.
func (s *Service) loadSalt(ctx context.Context) error {
	if !s.crypto.Enabled() {
		return nil
	}
	params, err := s.db.GetLocal(ctx, "obsidian_livesync_sync_parameters")
	if err != nil {
		if errors.Is(err, couchdb.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("loadSalt: %w", err)
	}
	if saltB64, ok := params["pbkdf2salt"].(string); ok {
		saltBytes, err := base64.StdEncoding.DecodeString(saltB64)
		if err == nil {
			s.crypto.SetSalt(saltBytes)
		}
	}
	return nil
}

// detectObfuscation sets s.obfuscated if any doc in docs has an f: prefixed ID,
// but only when obfuscationMode is "auto".
func (s *Service) detectObfuscation(docs []couchdb.MetaDoc) {
	if s.obfuscationMode != "auto" {
		return
	}
	for _, doc := range docs {
		if livesync.IsObfuscatedDocID(doc.ID) {
			s.obfuscated = true
			return
		}
	}
}

// decryptDocPaths decrypts the Path field of each MetaDoc whose path is obfuscated
// (starts with "/\:") and updates all metadata fields from the encrypted blob.
// When "Obfuscate Properties" is on, Obsidian Livesync stores path, children, mtime,
// ctime, and size inside the encrypted blob; the outer doc fields may be empty/zero.
// Errors are silently skipped; the encrypted value is left as-is so the caller can
// decide how to handle it.
func (s *Service) decryptDocPaths(docs []couchdb.MetaDoc) {
	for i := range docs {
		if !strings.HasPrefix(docs[i].Path, "/\\:") {
			continue
		}
		blob, err := s.crypto.DecryptPathBlob(docs[i].Path)
		if err != nil || blob == nil {
			continue
		}
		docs[i].Path = blob.Path
		// Restore metadata that Obsidian moved into the encrypted blob.
		if len(blob.Children) > 0 {
			docs[i].Children = blob.Children
		}
		if blob.MTime > 0 {
			docs[i].MTime = blob.MTime
		}
		if blob.CTime > 0 {
			docs[i].CTime = blob.CTime
		}
		if blob.Size > 0 {
			docs[i].Size = blob.Size
		}
	}
}

// List returns the meta documents in the remote vault, optionally filtered by a
// vault-relative path prefix. prefix="" returns all documents; prefix ending
// with "/" returns documents inside that folder; a bare filename returns only
// that file (exact path match). Results are sorted by path.
func (s *Service) List(ctx context.Context, prefix string) ([]couchdb.MetaDoc, error) {
	if err := s.loadSalt(ctx); err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	docs, err := s.db.AllMetaDocs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	s.decryptDocPaths(docs)
	var result []couchdb.MetaDoc
	for _, doc := range docs {
		if doc.IsDeleted() {
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

// tombstonePrefix marks every live remote document whose path lies under prefix
// (which must end in "/") as app-level deleted. Used when a directory
// disappears locally: the kernel reports one event for the directory and none
// for its children, so the children have to be resolved from the remote side.
func (s *Service) tombstonePrefix(ctx context.Context, prefix string) {
	docs, err := s.db.AllMetaDocs(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watch: tombstone %q: list: %v\n", prefix, err)
		return
	}
	s.decryptDocPaths(docs)
	for _, doc := range docs {
		if doc.IsDeleted() || !strings.HasPrefix(doc.Path, prefix) {
			continue
		}
		// Re-fetch the raw document: decryptDocPaths replaced Path with the
		// cleartext path in place and PutMeta serialises verbatim, so writing
		// this copy back would leak the path on an obfuscated vault.
		raw, gerr := s.db.GetMeta(ctx, doc.ID)
		if gerr != nil {
			fmt.Fprintf(os.Stderr, "watch: tombstone %q: fetch: %v\n", doc.Path, gerr)
			continue
		}
		raw.Deleted = false
		raw.DeletedApp = true
		raw.Children = nil
		if _, err := s.db.PutMeta(ctx, raw); err != nil {
			fmt.Fprintf(os.Stderr, "watch: tombstone %q: %v\n", doc.Path, err)
			continue
		}
		if s.OnTombstone != nil {
			s.OnTombstone(doc.Path)
		}
	}
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
		// Skip true non-file documents (chunks h:..., internal i:..., ix:...).
		// f: docs are obfuscated file meta-docs and must NOT be skipped here.
		_, isFile := livesync.DecodeDocID(event.ID)
		if !isFile {
			return nil
		}

		// Resolve path: prefer the doc body field, decrypt if obfuscated.
		// Also restore Children and other metadata from the encrypted blob —
		// Obsidian Livesync stores chunk IDs inside the blob when obfuscation is on.
		path := ""
		if event.Doc != nil {
			path = event.Doc.Path
		}
		if strings.HasPrefix(path, "/\\:") {
			blob, err := s.crypto.DecryptPathBlob(path)
			if err != nil {
				return fmt.Errorf("watch: decrypt path for %q: %w", event.ID, err)
			}
			if blob != nil {
				path = blob.Path
				if event.Doc != nil {
					if len(blob.Children) > 0 {
						event.Doc.Children = blob.Children
					}
					if blob.MTime > 0 {
						event.Doc.MTime = blob.MTime
					}
					if blob.CTime > 0 {
						event.Doc.CTime = blob.CTime
					}
					if blob.Size > 0 {
						event.Doc.Size = blob.Size
					}
				}
			}
		}
		// Fallback to decoding the doc ID (only useful for plain IDs, not f: hashes).
		if path == "" {
			path, _ = livesync.DecodeDocID(event.ID)
		}
		// Skip if path could not be resolved (e.g. lean tombstone for an obfuscated doc).
		if path == "" || livesync.IsObfuscatedDocID(path) {
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
		// Must use the obfuscation-aware encoder: on a path-obfuscated vault the
		// document ID is "f:<sha256>", not the plain lowercased path. Using the
		// plain ID here made GetMeta return ErrNotFound for every delete, so
		// local deletions were silently dropped and never tombstoned remotely.
		docID := s.encodeDocID(relPath)
		existing, err := s.db.GetMeta(ctx, docID)
		if err != nil {
			if !errors.Is(err, couchdb.ErrNotFound) {
				fmt.Fprintf(os.Stderr, "watch: delete %q: %v\n", relPath, err)
				return
			}
			// No document at this exact path, so the entry that disappeared was
			// most likely a directory. A directory rename/removal arrives as a
			// single event with no per-child events, so every document beneath
			// it must be tombstoned here — otherwise the old tree stays live in
			// CouchDB and is resurrected on every other device.
			s.tombstonePrefix(ctx, relPath+"/")
			return
		}
		existing.Deleted = false
		existing.DeletedApp = true
		existing.Children = nil
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
		lw := watcher.NewLocalWatcher(s.dataDir, s.suppress, localOnChange, localOnRemove, watcher.WithSyncHidden(s.syncHidden))

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
	lw := watcher.NewLocalWatcher(s.dataDir, s.suppress, localOnChange, localOnRemove, watcher.WithSyncHidden(s.syncHidden))
	return lw.Run(ctx)
}
