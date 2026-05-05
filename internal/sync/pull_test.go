package sync_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jookos/obgo-sync/internal/couchdb"
	"github.com/jookos/obgo-sync/internal/crypto"
	syncsvc "github.com/jookos/obgo-sync/internal/sync"
)

func TestPull_EmptyVault(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull with empty vault: %v", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no files, got %d", len(entries))
	}
}

func TestPull_OneDocOneChunk(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	content := "hello from CouchDB"
	chunkID := "h:abc123"
	db.chunkDocs[chunkID] = couchdb.ChunkDoc{
		ID:   chunkID,
		Data: content, // plain text docs store raw UTF-8 in the data field
		Type: "leaf",
	}
	db.metaDocs = []couchdb.MetaDoc{
		{
			ID:       "notes/hello.md",
			Type:     "plain",
			Path:     "notes/hello.md",
			Children: []string{chunkID},
		},
	}

	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	absPath := filepath.Join(tmpDir, "notes", "hello.md")
	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != content {
		t.Errorf("file content mismatch: got %q, want %q", got, content)
	}
}

func TestPull_MultiChunk(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	part1 := "chunk one content "
	part2 := "chunk two content"
	id1 := "h:chunk1"
	id2 := "h:chunk2"
	// plain type docs store raw UTF-8 text in the data field
	db.chunkDocs[id1] = couchdb.ChunkDoc{ID: id1, Data: part1, Type: "leaf"}
	db.chunkDocs[id2] = couchdb.ChunkDoc{ID: id2, Data: part2, Type: "leaf"}
	db.metaDocs = []couchdb.MetaDoc{
		{
			ID:       "multi.txt",
			Type:     "plain",
			Path:     "multi.txt",
			Children: []string{id1, id2},
		},
	}

	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(tmpDir, "multi.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := part1 + part2
	if string(got) != want {
		t.Errorf("multi-chunk mismatch: got %q, want %q", got, want)
	}
}

func TestPull_SingleFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	id1, id2 := "h:c1", "h:c2"
	db.chunkDocs[id1] = couchdb.ChunkDoc{ID: id1, Data: "wanted", Type: "leaf"}
	db.chunkDocs[id2] = couchdb.ChunkDoc{ID: id2, Data: "other", Type: "leaf"}
	db.metaDocs = []couchdb.MetaDoc{
		{ID: "notes/wanted.md", Type: "plain", Path: "notes/wanted.md", Children: []string{id1}},
		{ID: "notes/other.md", Type: "plain", Path: "notes/other.md", Children: []string{id2}},
	}

	if err := svc.Pull(context.Background(), "notes/wanted.md"); err != nil {
		t.Fatalf("Pull single file: %v", err)
	}

	// Only the requested file should be written.
	if _, err := os.Stat(filepath.Join(tmpDir, "notes", "wanted.md")); err != nil {
		t.Errorf("expected notes/wanted.md to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "notes", "other.md")); !os.IsNotExist(err) {
		t.Error("notes/other.md should not have been pulled")
	}
}

// TestPull_DeletesLocalFileForLeanTombstone covers the case where Obsidian/PouchDB
// deletes a document via HTTP DELETE, producing a lean tombstone with no path or
// type field — only _id, _rev, and _deleted:true.
func TestPull_DeletesLocalFileForLeanTombstone(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	// Create the local file that was deleted on the remote.
	absPath := filepath.Join(tmpDir, "lean-deleted.md")
	if err := os.WriteFile(absPath, []byte("old content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Lean tombstone: no Path, no Type — only ID and Deleted flag.
	db.metaDocs = []couchdb.MetaDoc{
		{ID: "lean-deleted.md", Deleted: true},
	}

	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Error("expected local file to be removed after pulling lean tombstone")
	}
}

func TestPull_DeletesLocalFileForRemoteTombstone(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	// Create a local file that was deleted on the remote.
	absPath := filepath.Join(tmpDir, "deleted.md")
	if err := os.WriteFile(absPath, []byte("stale content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Mock returns a tombstone doc for the file.
	db.metaDocs = []couchdb.MetaDoc{
		{
			ID:      "deleted.md",
			Type:    "plain",
			Path:    "deleted.md",
			Deleted: true,
		},
	}

	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Error("expected local file to be removed after pulling tombstone")
	}
}

// TestPull_DeletesLocalFileForAppLevelTombstone covers the Livesync app-level
// deletion format: a full document with deleted:true (no underscore) that
// preserves all metadata fields. This is what the official Obsidian Livesync
// plugin writes — it avoids CouchDB-level tombstones so that path info survives.
func TestPull_DeletesLocalFileForAppLevelTombstone(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	absPath := filepath.Join(tmpDir, "app-deleted.md")
	if err := os.WriteFile(absPath, []byte("old content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// App-level tombstone: full doc with deleted:true, no _deleted.
	db.metaDocs = []couchdb.MetaDoc{
		{
			ID:         "app-deleted.md",
			Type:       "plain",
			Path:       "app-deleted.md",
			DeletedApp: true,
		},
	}

	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Error("expected local file to be removed after pulling app-level tombstone")
	}
}

func TestPull_FolderPath(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	id1, id2, id3 := "h:n1", "h:n2", "h:p1"
	db.chunkDocs[id1] = couchdb.ChunkDoc{ID: id1, Data: "note1", Type: "leaf"}
	db.chunkDocs[id2] = couchdb.ChunkDoc{ID: id2, Data: "note2", Type: "leaf"}
	db.chunkDocs[id3] = couchdb.ChunkDoc{ID: id3, Data: "proj1", Type: "leaf"}
	db.metaDocs = []couchdb.MetaDoc{
		{ID: "notes/a.md", Type: "plain", Path: "notes/a.md", Children: []string{id1}},
		{ID: "notes/b.md", Type: "plain", Path: "notes/b.md", Children: []string{id2}},
		{ID: "projects/x.md", Type: "plain", Path: "projects/x.md", Children: []string{id3}},
	}

	if err := svc.Pull(context.Background(), "notes/"); err != nil {
		t.Fatalf("Pull folder: %v", err)
	}

	for _, name := range []string{"notes/a.md", "notes/b.md"} {
		if _, err := os.Stat(filepath.Join(tmpDir, filepath.FromSlash(name))); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "projects", "x.md")); !os.IsNotExist(err) {
		t.Error("projects/x.md should not have been pulled")
	}
}

// TestPull_ObfuscatedPath verifies that a MetaDoc with an f:-prefixed ID and an
// encrypted "/\:..." path field is correctly decrypted and written to disk.
func TestPull_ObfuscatedPath(t *testing.T) {
	const password = "test-obfuscate-pass"
	const saltStr = "test-pull-salt-32-bytes-padding!"

	cr := crypto.New(password)
	cr.SetSalt([]byte(saltStr))

	// Build an encrypted path blob identical to what Obsidian Livesync produces.
	realPath := "notes/secret.md"
	encPath, err := cr.EncryptPath(realPath, 1000, 2000, 7, []string{"h:+chunkA"})
	if err != nil {
		t.Fatalf("EncryptPath: %v", err)
	}
	if !strings.HasPrefix(encPath, "/\\:") {
		t.Fatalf("encrypted path should start with /\\:, got %q", encPath[:10])
	}

	// Build an encrypted chunk doc.
	chunkData, err := cr.EncryptContent([]byte("content!"))
	if err != nil {
		t.Fatalf("EncryptContent: %v", err)
	}

	tmpDir := t.TempDir()
	db := newMockClient()
	// Seed the sync-parameters local doc so loadSalt can find the salt.
	db.localDocs["obsidian_livesync_sync_parameters"] = map[string]interface{}{
		"pbkdf2salt": base64.StdEncoding.EncodeToString([]byte(saltStr)),
	}

	chunkID := "h:+chunkA"
	db.chunkDocs[chunkID] = couchdb.ChunkDoc{ID: chunkID, Data: chunkData, Type: "leaf", Encrypted: true}
	db.metaDocs = []couchdb.MetaDoc{
		{
			ID:        cr.ObfuscateDocID(realPath),
			Type:      "plain",
			Path:      encPath,
			Children:  []string{chunkID},
			Encrypted: true,
		},
	}

	svc := syncsvc.New(db, cr, tmpDir)
	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull with obfuscated path: %v", err)
	}

	// The file should appear at the decrypted path on disk.
	absPath := filepath.Join(tmpDir, filepath.FromSlash(realPath))
	got, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("ReadFile %q: %v", absPath, err)
	}
	if string(got) != "content!" {
		t.Errorf("file content mismatch: got %q, want %q", got, "content!")
	}
}

// TestPull_ObfuscatedPath_ChildrenInBlob mirrors the real Obsidian Livesync behaviour
// where Children on the outer MetaDoc is empty and chunk IDs are only in the encrypted blob.
func TestPull_ObfuscatedPath_ChildrenInBlob(t *testing.T) {
	const password = "test-obfuscate-pass"
	const saltStr = "test-pull-salt-32-bytes-padding!"

	cr := crypto.New(password)
	cr.SetSalt([]byte(saltStr))

	realPath := "notes/secret.md"
	chunkID := "h:+chunkA"
	// Encrypt path blob that includes children — outer MetaDoc children will be empty.
	encPath, err := cr.EncryptPath(realPath, 1000, 2000, 8, []string{chunkID})
	if err != nil {
		t.Fatalf("EncryptPath: %v", err)
	}

	chunkData, err := cr.EncryptContent([]byte("content!"))
	if err != nil {
		t.Fatalf("EncryptContent: %v", err)
	}

	tmpDir := t.TempDir()
	db := newMockClient()
	db.localDocs["obsidian_livesync_sync_parameters"] = map[string]interface{}{
		"pbkdf2salt": base64.StdEncoding.EncodeToString([]byte(saltStr)),
	}
	db.chunkDocs[chunkID] = couchdb.ChunkDoc{ID: chunkID, Data: chunkData, Type: "leaf", Encrypted: true}
	db.metaDocs = []couchdb.MetaDoc{
		{
			ID:        cr.ObfuscateDocID(realPath),
			Type:      "plain",
			Path:      encPath,
			Children:  nil, // intentionally empty — only in the encrypted blob
			Encrypted: true,
		},
	}

	svc := syncsvc.New(db, cr, tmpDir)
	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull with children-in-blob: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(tmpDir, filepath.FromSlash(realPath)))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "content!" {
		t.Errorf("file content: got %q, want %q", got, "content!")
	}
}

// TestPull_ObfuscatedTombstone verifies that a lean tombstone with an f: ID
// (which cannot be resolved to a path) is silently skipped without error.
func TestPull_ObfuscatedTombstone(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	// A lean tombstone: only _id, _deleted — no path, no type.
	db.metaDocs = []couchdb.MetaDoc{
		{ID: "f:deadbeef0123456789abcdef", Deleted: true},
	}
	svc := syncsvc.New(db, cr, tmpDir)
	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull with obfuscated tombstone should not error: %v", err)
	}
}

// TestPull_EncryptedBinary verifies that a binary file (type: newnote) in an
// encrypted vault is correctly decrypted and then base64-decoded before being
// written to disk. This fixes a bug where binary files were left as base64
// strings on disk.
func TestPull_EncryptedBinary(t *testing.T) {
	const password = "test-pass"
	const saltStr = "test-salt-32-bytes-padding!!!!!!"
	cr := crypto.New(password)
	cr.SetSalt([]byte(saltStr))

	rawBytes := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE}
	base64Encoded := base64.StdEncoding.EncodeToString(rawBytes)
	
	// Encrypt the base64 string, as Livesync does for binary chunks.
	encryptedChunk, err := cr.EncryptContent([]byte(base64Encoded))
	if err != nil {
		t.Fatalf("EncryptContent: %v", err)
	}

	tmpDir := t.TempDir()
	db := newMockClient()
	db.localDocs["obsidian_livesync_sync_parameters"] = map[string]interface{}{
		"pbkdf2salt": base64.StdEncoding.EncodeToString([]byte(saltStr)),
	}

	chunkID := "h:+chunkB"
	db.chunkDocs[chunkID] = couchdb.ChunkDoc{ID: chunkID, Data: encryptedChunk, Type: "leaf", Encrypted: true}
	db.metaDocs = []couchdb.MetaDoc{
		{
			ID:       "binary.png",
			Type:     "newnote", // Binary file
			Path:     "binary.png",
			Children: []string{chunkID},
		},
	}

	svc := syncsvc.New(db, cr, tmpDir)
	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(tmpDir, "binary.png"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, rawBytes) {
		t.Errorf("content mismatch: got %v, want %v", got, rawBytes)
		t.Logf("got (as string): %q", string(got))
	}
}
