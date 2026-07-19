package sync_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jookos/obgo-sync/internal/couchdb"
	"github.com/jookos/obgo-sync/internal/crypto"
	syncsvc "github.com/jookos/obgo-sync/internal/sync"
)

// Renaming a directory out of the vault produces a single event for the
// directory and no events for its children, and no document exists at the
// directory's own path. Every document beneath it must therefore be tombstoned
// from the prefix, otherwise the whole old tree stays live in CouchDB and is
// resurrected on every other device — which is exactly what a folder
// reorganisation looks like.
func TestWatch_DirectoryRemovalTombstonesSubtree(t *testing.T) {
	const password = "test-obfuscate-pass"
	const saltStr = "test-watch-salt-32-bytes-paddin!"

	cr := crypto.New(password)
	cr.SetSalt([]byte(saltStr))

	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "Lit"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths := []string{"Lit/a.md", "Lit/b.md", "keep.md"}
	db := newMockClient()
	db.localDocs["obsidian_livesync_sync_parameters"] = map[string]interface{}{
		"pbkdf2salt": base64.StdEncoding.EncodeToString([]byte(saltStr)),
	}
	ids := make(map[string]string, len(paths))
	for _, p := range paths {
		if err := os.WriteFile(filepath.Join(tmpDir, filepath.FromSlash(p)), []byte("body"), 0o644); err != nil {
			t.Fatal(err)
		}
		encPath, err := cr.EncryptPath(p, 1000, 2000, 4, []string{"h:+c"})
		if err != nil {
			t.Fatalf("EncryptPath %q: %v", p, err)
		}
		id := cr.ObfuscateDocID(p)
		ids[p] = id
		db.metaDocs = append(db.metaDocs, couchdb.MetaDoc{
			ID: id, Type: "plain", Path: encPath, Rev: "1-aaa",
			Children: []string{"h:+c"}, Encrypted: true,
		})
	}

	svc := syncsvc.New(db, cr, tmpDir)
	svc.SetObfuscationMode("on")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Watch(ctx, true, false) }()

	time.Sleep(200 * time.Millisecond)

	// Rename (not delete) the directory away: this is the case that emits no
	// per-child events.
	outside := t.TempDir()
	if err := os.Rename(filepath.Join(tmpDir, "Lit"), filepath.Join(outside, "Lit")); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)

	cancel()
	<-done

	tombstoned := make(map[string]bool, len(db.putMetaDocs))
	for _, d := range db.putMetaDocs {
		if d.DeletedApp {
			tombstoned[d.ID] = true
		}
	}

	for _, p := range []string{"Lit/a.md", "Lit/b.md"} {
		if !tombstoned[ids[p]] {
			t.Errorf("%q under the removed directory was not tombstoned (%d tombstones total)", p, len(tombstoned))
		}
	}
	if tombstoned[ids["keep.md"]] {
		t.Error("keep.md is outside the removed directory and must not be tombstoned")
	}
}
