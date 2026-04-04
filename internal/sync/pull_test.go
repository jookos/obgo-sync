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

	if err := svc.Pull(context.Background()); err != nil {
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

	if err := svc.Pull(context.Background()); err != nil {
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

	if err := svc.Pull(context.Background()); err != nil {
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
