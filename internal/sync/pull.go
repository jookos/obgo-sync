package sync

import (
	"context"
	"encoding/base64"
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
	if err := s.loadSalt(ctx); err != nil {
		return fmt.Errorf("pull: %w", err)
	}

	// 2. Fetch all meta docs; detect + handle obfuscation before any filtering.
	docs, err := s.db.AllMetaDocs(ctx)
	if err != nil {
		return fmt.Errorf("pull: list docs: %w", err)
	}
	s.detectObfuscation(docs)
	s.decryptDocPaths(docs)

	// 3. Single-file shortcut: find the requested doc in the already-loaded list.
	if filter != "" && !strings.HasSuffix(filter, "/") {
		for _, doc := range docs {
			if doc.Path == filter {
				resolved, rerr := s.resolveConflicts(ctx, doc)
				if rerr != nil {
					fmt.Fprintf(os.Stderr, "pull: resolve conflicts %q: %v\n", doc.Path, rerr)
					resolved = doc
				}
				if err := s.applyRemoteDoc(ctx, resolved); err != nil {
					return fmt.Errorf("pull: apply %q: %w", doc.Path, err)
				}
				if s.OnPullFile != nil {
					s.OnPullFile(1)
				}
				return nil
			}
		}
		return fmt.Errorf("pull: %q not found in remote vault", filter)
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
// writes it to the local filesystem. If the doc signals deletion (either a
// CouchDB tombstone _deleted:true or a Livesync app-level deleted:true field),
// the corresponding local file is removed instead.
func (s *Service) applyRemoteDoc(ctx context.Context, doc couchdb.MetaDoc) error {
	// Decrypt obfuscated path field ("/\:" prefix) if present.
	// decryptDocPaths does this in bulk for the main loop, but watch mode and
	// conflict-resolved docs may call applyRemoteDoc directly.
	// Important: also restore Children from the blob — Obsidian Livesync stores
	// chunk IDs inside the encrypted blob when "Obfuscate Properties" is on.
	if strings.HasPrefix(doc.Path, "/\\:") {
		blob, err := s.crypto.DecryptPathBlob(doc.Path)
		if err != nil {
			return fmt.Errorf("decrypt path: %w", err)
		}
		if blob != nil {
			doc.Path = blob.Path
			if len(blob.Children) > 0 {
				doc.Children = blob.Children
			}
			if blob.MTime > 0 {
				doc.MTime = blob.MTime
			}
			if blob.CTime > 0 {
				doc.CTime = blob.CTime
			}
			if blob.Size > 0 {
				doc.Size = blob.Size
			}
		}
	}

	// Handle remote deletions: remove the local file.
	if doc.Deleted || doc.DeletedApp {
		// Lean tombstones (from Obsidian/PouchDB HTTP DELETE) have no path field;
		// fall back to decoding the document ID.
		path := doc.Path
		if path == "" {
			path, _ = livesync.DecodeDocID(doc.ID)
		}
		// Skip if path resolved to an opaque f: hash (can't find the local file).
		if path == "" || livesync.IsObfuscatedDocID(path) {
			return nil
		}
		// Mirrors the push-side policy: never touch .git/.obgo* locally, and
		// leave hidden paths alone unless this device opted in. Without this,
		// a hidden file another (opted-in) device deleted remotely could still
		// trigger a local remove/suppress on a device that never pulled it.
		if shouldSkipLocalPath(path, "", s.syncHidden) {
			return nil
		}
		// resolveCase handles case mismatches between the lowercase doc ID
		// and the original-cased path stored on a case-sensitive filesystem.
		absPath := resolveCase(s.dataDir, path)
		s.suppress.Add(absPath)
		_ = os.Remove(absPath)
		if s.OnDeleteFile != nil {
			s.OnDeleteFile(path)
		}
		return nil
	}
	// Skip internal state files, and hidden paths unless this device opted
	// in via OBGO_SYNC_HIDDEN — otherwise a device with the flag off would
	// pull a file it will then refuse to push or delete (see push.go).
	if shouldSkipLocalPath(doc.Path, "", s.syncHidden) {
		return nil
	}
	// Fetch chunks.
	chunks, err := s.db.BulkGet(ctx, doc.Children)
	if err != nil {
		return fmt.Errorf("fetch chunks: %w", err)
	}

	// Build a map for ordering; BulkGet does not guarantee order.
	chunkMap := make(map[string]couchdb.ChunkDoc, len(chunks))
	for _, c := range chunks {
		chunkMap[c.ID] = c
	}

	// Assemble content: each chunk is decrypted (if vault is encrypted) and
	// then decoded (if file is binary) before concatenation.
	var content []byte
	for _, id := range doc.Children {
		chunk, ok := chunkMap[id]
		if !ok {
			return fmt.Errorf("missing chunk %q", id)
		}
		data := chunk.Data

		var piece []byte
		var err error
		// Decrypt only chunks actually flagged encrypted (e_). This vault is a
		// MIX of encrypted and plaintext chunks — E2EE was enabled on an
		// already-populated vault without a full rebuild, so old chunks remain
		// plaintext and some even start with "%" (e.g. Templater directives).
		// The ciphertext-prefix heuristic is therefore unreliable; the per-chunk
		// e_ flag is the only correct signal, and it is what the official
		// LiveSync client keys off.
		if s.crypto.Enabled() && chunk.Encrypted {
			piece, err = s.crypto.DecryptContent(data)
			if err != nil {
				return fmt.Errorf("decrypt chunk %q: %w", id, err)
			}
		} else {
			piece = []byte(data)
		}

		if doc.Type == "newnote" {
			// Binary file: chunks are base64-encoded (either raw or inside ciphertext).
			decoded, err := base64.StdEncoding.DecodeString(string(piece))
			if err != nil {
				return fmt.Errorf("decode chunk %q: %w", id, err)
			}
			piece = decoded
		}
		content = append(content, piece...)
	}

	// Write to disk.
	absPath := filepath.Join(s.dataDir, filepath.FromSlash(doc.Path))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	s.suppress.Add(absPath)
	return os.WriteFile(absPath, content, 0o644)
}
