package sync

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/jookos/obgo-sync/internal/couchdb"
	"github.com/jookos/obgo-sync/lib/livesync"
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
	var count int
	return filepath.WalkDir(s.dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Skip internal state files.
		if name := filepath.Base(path); len(name) > 5 && name[:5] == ".obgo" {
			return nil
		}
		if err := s.pushFile(ctx, path); err != nil {
			return err
		}
		count++
		if s.OnPushFile != nil {
			s.OnPushFile(count)
		}
		return nil
	})
}

// ensureSalt loads the HKDF salt from CouchDB or generates and stores a new one.
func (s *Service) ensureSalt(ctx context.Context) error {
	params, err := s.db.GetLocal(ctx, "obsidian_livesync_sync_parameters")
	if err != nil && !errors.Is(err, couchdb.ErrNotFound) {
		return err
	}
	if params != nil {
		if saltB64, ok := params["pbkdf2salt"].(string); ok {
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
		"pbkdf2salt": base64.StdEncoding.EncodeToString(salt),
	}
	if rev != "" {
		doc["_rev"] = rev
	}
	return s.db.PutLocal(ctx, "obsidian_livesync_sync_parameters", doc)
}

// pushFile reads a local file, splits it into chunks compatible with the
// Obsidian Livesync protocol, uploads them via BulkDocs, then upserts the
// meta document.
//
// Plain text files are split at DefaultTextChunkSize (1000) Unicode code
// points; each chunk's data field is the raw UTF-8 string.
//
// Binary files are base64-encoded as a whole, then split at 102400 characters;
// each chunk's data field is the base64 substring.
//
// Chunk IDs are computed with crypto.ChunkID, which uses xxHash-64 of
// "${data}-${charCount}" encoded as base36 — matching Obsidian's algorithm.
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

	// Determine doc type: plain (text) or newnote (binary).
	isText := utf8.Valid(content)
	docType := "newnote"
	if isText {
		docType = "plain"
	}

	// Build data chunks (the strings stored in each chunk doc's data field).
	//
	// Obsidian plain:   split UTF-8 text at 1000 code points
	// Obsidian newnote: base64-encode full file, split at 102400 chars
	var dataChunks []string
	if isText {
		dataChunks = livesync.SplitText(string(content), 0)
	} else {
		encoded := base64.StdEncoding.EncodeToString(content)
		for len(encoded) > 0 {
			size := livesync.DefaultChunkSize
			if size > len(encoded) {
				size = len(encoded)
			}
			dataChunks = append(dataChunks, encoded[:size])
			encoded = encoded[size:]
		}
	}

	chunkIDs := make([]string, 0, len(dataChunks))
	chunkDocs := make([]interface{}, 0, len(dataChunks))

	for _, chunk := range dataChunks {
		id := s.crypto.ChunkID(chunk)
		chunkIDs = append(chunkIDs, id)

		var data string
		if s.crypto.Enabled() {
			data, err = s.crypto.EncryptContent([]byte(chunk))
			if err != nil {
				return fmt.Errorf("encrypt chunk: %w", err)
			}
		} else {
			data = chunk
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
		Type:      docType,
		Path:      relPath,
		CTime:     ctime,
		MTime:     mtime,
		Size:      info.Size(),
		Children:  chunkIDs,
		Eden:      map[string]interface{}{},
		Encrypted: s.crypto.Enabled(),
	}
	if _, err := s.db.PutMeta(ctx, meta); err != nil {
		return fmt.Errorf("put meta %q: %w", relPath, err)
	}
	return nil
}
