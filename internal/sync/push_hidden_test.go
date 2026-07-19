package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jookos/obgo-sync/internal/crypto"
	syncsvc "github.com/jookos/obgo-sync/internal/sync"
)

// The local watcher ignores every dotted path, so the push walk must too.
// A real vault routinely contains .git (often tens of thousands of objects),
// .obsidian and .trash; uploading those into the vault database would push
// them out to every synced device.
func TestPush_SkipsHiddenPaths(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	for _, dir := range []string{".git/objects", ".obsidian", ".trash"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("MkdirAll %q: %v", dir, err)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmpDir, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %q: %v", rel, err)
		}
	}
	write("note.md", "keep me")
	write(".git/objects/deadbeef", "git object")
	write(".obsidian/app.json", "{}")
	write(".trash/old.md", "trashed")
	write(".hidden.md", "hidden file")
	write(".obgo_seq", "42")

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}

	var pushed []string
	for _, d := range db.putMetaDocs {
		pushed = append(pushed, d.Path)
	}
	if len(pushed) != 1 || pushed[0] != "note.md" {
		t.Fatalf("only note.md should be pushed, got %v", pushed)
	}
}
