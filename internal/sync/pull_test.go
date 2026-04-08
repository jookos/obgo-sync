package sync_test

import (
	"context"
	"os"
	"path/filepath"
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
