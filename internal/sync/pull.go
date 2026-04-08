package sync

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jookos/obgo-sync/internal/couchdb"
	"github.com/jookos/obgo-sync/lib/livesync"
)

// Pull fetches remote documents and writes them to OBGO_DATA.
// filter is a vault-relative path: empty means all docs, a path ending with "/"
// pulls that folder and its contents, otherwise pulls the single named file.
// If E2EE is enabled, it loads the HKDF salt from CouchDB _local doc first.
func (s *Service) Pull(ctx context.Context, filter string) error {
	// 1. Load HKDF salt from CouchDB _local doc.
	if s.crypto.Enabled() {
		params, err := s.db.GetLocal(ctx, "obsidian_livesync_sync_parameters")
		if err != nil && !errors.Is(err, couchdb.ErrNotFound) {
			return fmt.Errorf("pull: load salt: %w", err)
		}
		if params != nil {
			if saltB64, ok := params["pbkdf2salt"].(string); ok {
				saltBytes, err := base64.StdEncoding.DecodeString(saltB64)
				if err == nil {
					s.crypto.SetSalt(saltBytes)
				}
			}
		}
	}

	// 2. Single-file shortcut: fetch just that document by ID.
	if filter != "" && !strings.HasSuffix(filter, "/") {
		docID := livesync.EncodeDocID(filter)
		doc, err := s.db.GetMeta(ctx, docID)
		if err != nil {
			if errors.Is(err, couchdb.ErrNotFound) {
				return fmt.Errorf("pull: %q not found in remote vault", filter)
			}
			return fmt.Errorf("pull: get %q: %w", filter, err)
		}
		resolved, rerr := s.resolveConflicts(ctx, *doc)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "pull: resolve conflicts %q: %v\n", doc.Path, rerr)
			resolved = *doc
		}
		if err := s.applyRemoteDoc(ctx, resolved); err != nil {
			return fmt.Errorf("pull: apply %q: %w", doc.Path, err)
		}
		if s.OnPullFile != nil {
			s.OnPullFile(1)
		}
		return nil
	}

	// 3. Fetch all meta docs (used for both all-docs and folder-prefix cases).
	docs, err := s.db.AllMetaDocs(ctx)
	if err != nil {
		return fmt.Errorf("pull: list docs: %w", err)
	}

	// 4. For each meta doc, apply to disk (skipping those outside the folder filter).
	var count int
	for _, doc := range docs {
		if filter != "" && !strings.HasPrefix(doc.Path, filter) {
			continue
		}
		resolved, rerr := s.resolveConflicts(ctx, doc)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "pull: resolve conflicts %q: %v\n", doc.Path, rerr)
			resolved = doc
		}
		if err := s.applyRemoteDoc(ctx, resolved); err != nil {
			return fmt.Errorf("pull: apply %q: %w", doc.Path, err)
		}
		count++
		if s.OnPullFile != nil {
			s.OnPullFile(count)
		}
	}

	return nil
}

// applyRemoteDoc fetches chunks for a meta doc, assembles the content and
// writes it to the local filesystem. If the doc is a tombstone (_deleted:true),
// the corresponding local file is removed instead.
func (s *Service) applyRemoteDoc(ctx context.Context, doc couchdb.MetaDoc) error {
	// Handle remote deletions: remove the local file.
	if doc.Deleted {
		// Lean tombstones (from Obsidian/PouchDB HTTP DELETE) have no path field;
		// fall back to decoding the document ID.
		path := doc.Path
		if path == "" {
			path, _ = livesync.DecodeDocID(doc.ID)
		}
		if path != "" {
			absPath := filepath.Join(s.dataDir, filepath.FromSlash(path))
			s.suppress.Add(absPath)
			_ = os.Remove(absPath)
			if s.OnDeleteFile != nil {
				s.OnDeleteFile(path)
			}
		}
		return nil
	}
	// Skip internal state files that should never be synced.
	if base := filepath.Base(doc.Path); len(base) > 5 && base[:5] == ".obgo" {
		return nil
	}
	// Fetch chunks.
	chunks, err := s.db.BulkGet(ctx, doc.Children)
	if err != nil {
		return fmt.Errorf("fetch chunks: %w", err)
	}

	// Build a map for ordering; BulkGet does not guarantee order.
	chunkMap := make(map[string]string, len(chunks))
	for _, c := range chunks {
		chunkMap[c.ID] = c.Data
	}

	// Assemble content: each chunk is decrypted (or base64-decoded) individually
	// and the raw bytes are concatenated.
	var content []byte
	for _, id := range doc.Children {
		data, ok := chunkMap[id]
		if !ok {
			return fmt.Errorf("missing chunk %q", id)
		}

		if s.crypto.Enabled() {
			plaintext, err := s.crypto.DecryptContent(data)
			if err != nil {
				return fmt.Errorf("decrypt chunk %q: %w", id, err)
			}
			content = append(content, plaintext...)
		} else if doc.Type == "newnote" {
			// Binary file: chunks are base64-encoded.
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				return fmt.Errorf("decode chunk %q: %w", id, err)
			}
			content = append(content, decoded...)
		} else {
			// Plain text file: chunks are raw UTF-8 strings.
			content = append(content, []byte(data)...)
		}
	}

	// Write to disk.
	absPath := filepath.Join(s.dataDir, filepath.FromSlash(doc.Path))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	s.suppress.Add(absPath)
	return os.WriteFile(absPath, content, 0o644)
}
