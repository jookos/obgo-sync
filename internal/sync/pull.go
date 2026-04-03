package sync

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jookos/obgo/internal/couchdb"
)

// Pull fetches all remote documents and writes them to OBGO_DATA.
// If E2EE is enabled, it loads the HKDF salt from CouchDB _local doc first.
func (s *Service) Pull(ctx context.Context) error {
	// 1. Load HKDF salt from CouchDB _local doc.
	if s.crypto.Enabled() {
		params, err := s.db.GetLocal(ctx, "obsidian_livesync_sync_parameters")
		if err != nil && !errors.Is(err, couchdb.ErrNotFound) {
			return fmt.Errorf("pull: load salt: %w", err)
		}
		if params != nil {
			if saltB64, ok := params["salt"].(string); ok {
				saltBytes, err := base64.StdEncoding.DecodeString(saltB64)
				if err == nil {
					s.crypto.SetSalt(saltBytes)
				}
			}
		}
	}

	// 2. Fetch all meta docs.
	docs, err := s.db.AllMetaDocs(ctx)
	if err != nil {
		return fmt.Errorf("pull: list docs: %w", err)
	}

	// 3. For each meta doc, fetch chunks, assemble, decrypt, write to disk.
	for _, doc := range docs {
		if err := s.applyRemoteDoc(ctx, doc); err != nil {
			return fmt.Errorf("pull: apply %q: %w", doc.Path, err)
		}
	}

	return nil
}

// applyRemoteDoc fetches chunks for a meta doc, assembles the content and
// writes it to the local filesystem.
func (s *Service) applyRemoteDoc(ctx context.Context, doc couchdb.MetaDoc) error {
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
		} else {
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				return fmt.Errorf("decode chunk %q: %w", id, err)
			}
			content = append(content, decoded...)
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
