package sync

import (
	cryptorand "crypto/rand"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/jookos/obgo/internal/couchdb"
	"github.com/jookos/obgo/lib/livesync"
)

// Push reads all files from OBGO_DATA and upserts them to CouchDB.
func (s *Service) Push(ctx context.Context) error {
	// 1. Ensure salt is set (create if needed) when E2EE is enabled.
	if s.crypto.Enabled() {
		if err := s.ensureSalt(ctx); err != nil {
			return fmt.Errorf("push: salt: %w", err)
		}
	}

	// 2. Walk dataDir and push every file.
	return filepath.WalkDir(s.dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		return s.pushFile(ctx, path)
	})
}

// ensureSalt loads the HKDF salt from CouchDB or generates and stores a new one.
func (s *Service) ensureSalt(ctx context.Context) error {
	params, err := s.db.GetLocal(ctx, "obsidian_livesync_sync_parameters")
	if err != nil && !errors.Is(err, couchdb.ErrNotFound) {
		return err
	}
	if params != nil {
		if saltB64, ok := params["salt"].(string); ok {
			saltBytes, decErr := base64.StdEncoding.DecodeString(saltB64)
			if decErr == nil {
				s.crypto.SetSalt(saltBytes)
				return nil
			}
		}
	}

	// Generate new random salt and persist it.
	salt := make([]byte, 32)
	if _, err := cryptorand.Read(salt); err != nil {
		return fmt.Errorf("ensureSalt: generate: %w", err)
	}
	s.crypto.SetSalt(salt)

	rev := ""
	if params != nil {
		if r, ok := params["_rev"].(string); ok {
			rev = r
		}
	}
	doc := map[string]interface{}{
		"salt": base64.StdEncoding.EncodeToString(salt),
	}
	if rev != "" {
		doc["_rev"] = rev
	}
	return s.db.PutLocal(ctx, "obsidian_livesync_sync_parameters", doc)
}

// pushFile reads a local file, splits it into chunks, encrypts them (if E2EE
// is enabled), uploads them via BulkDocs, then upserts the meta document.
func (s *Service) pushFile(ctx context.Context, absPath string) error {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read %q: %w", absPath, err)
	}

	relPath, err := filepath.Rel(s.dataDir, absPath)
	if err != nil {
		return err
	}
	relPath = filepath.ToSlash(relPath)

	// Split into chunks and encrypt.
	rawChunks := livesync.Split(content, 0)
	chunkIDs := make([]string, 0, len(rawChunks))
	chunkDocs := make([]interface{}, 0, len(rawChunks))

	for _, chunk := range rawChunks {
		id := s.crypto.ChunkID(chunk)
		chunkIDs = append(chunkIDs, id)

		var data string
		if s.crypto.Enabled() {
			data, err = s.crypto.EncryptContent(chunk)
			if err != nil {
				return fmt.Errorf("encrypt chunk: %w", err)
			}
		} else {
			data = base64.StdEncoding.EncodeToString(chunk)
		}

		chunkDocs = append(chunkDocs, &couchdb.ChunkDoc{
			ID:        id,
			Data:      data,
			Type:      "leaf",
			Encrypted: s.crypto.Enabled(),
		})
	}

	// Upload chunks.
	if err := s.db.BulkDocs(ctx, chunkDocs); err != nil {
		return fmt.Errorf("upload chunks for %q: %w", relPath, err)
	}

	// Stat the file for timestamps.
	info, err := os.Stat(absPath)
	if err != nil {
		return err
	}
	mtime := info.ModTime().UnixMilli()

	// Fetch existing doc for rev and original ctime.
	docID := livesync.EncodeDocID(relPath)
	existing, err := s.db.GetMeta(ctx, docID)
	var rev string
	var ctime int64 = mtime
	if err == nil {
		rev = existing.Rev
		if existing.CTime > 0 {
			ctime = existing.CTime
		}
	} else if !errors.Is(err, couchdb.ErrNotFound) {
		return fmt.Errorf("get existing meta for %q: %w", relPath, err)
	}

	meta := &couchdb.MetaDoc{
		ID:        docID,
		Rev:       rev,
		Type:      "plain",
		Path:      relPath,
		CTime:     ctime,
		MTime:     mtime,
		Size:      info.Size(),
		Children:  chunkIDs,
		Encrypted: s.crypto.Enabled(),
	}
	if _, err := s.db.PutMeta(ctx, meta); err != nil {
		return fmt.Errorf("put meta %q: %w", relPath, err)
	}
	return nil
}

