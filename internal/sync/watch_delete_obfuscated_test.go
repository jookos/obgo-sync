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

// On a path-obfuscated vault the document ID is "f:<sha256>", not the plain
// lowercased path. Deriving the ID with the plain encoder makes the lookup miss
// every time, so local deletions are silently dropped and the remote document
// stays live — it then reappears on every other device.
func TestWatch_LocalDeleteTombstonesObfuscatedDoc(t *testing.T) {
	const password = "test-obfuscate-pass"
	const saltStr = "test-watch-salt-32-bytes-paddin!"

	cr := crypto.New(password)
	cr.SetSalt([]byte(saltStr))

	const realPath = "note.md"
	encPath, err := cr.EncryptPath(realPath, 1000, 2000, 5, []string{"h:+c"})
	if err != nil {
		t.Fatalf("EncryptPath: %v", err)
	}

	tmpDir := t.TempDir()
	absPath := filepath.Join(tmpDir, realPath)
	if err := os.WriteFile(absPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	obfID := cr.ObfuscateDocID(realPath)
	db := newMockClient()
	db.localDocs["obsidian_livesync_sync_parameters"] = map[string]interface{}{
		"pbkdf2salt": base64.StdEncoding.EncodeToString([]byte(saltStr)),
	}
	db.metaDocs = []couchdb.MetaDoc{{
		ID:        obfID,
		Type:      "plain",
		Path:      encPath,
		Rev:       "1-aaa",
		Children:  []string{"h:+c"},
		Encrypted: true,
	}}

	svc := syncsvc.New(db, cr, tmpDir)
	svc.SetObfuscationMode("on")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Watch(ctx, true, false) }()

	time.Sleep(200 * time.Millisecond) // let the watcher register
	if err := os.Remove(absPath); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	time.Sleep(1200 * time.Millisecond) // remove debounce (500ms) + processing

	cancel()
	<-done // ensures the watcher goroutine's writes are visible here

	if len(db.putMetaDocs) == 0 {
		t.Fatal("local delete produced no remote tombstone: the obfuscated document ID was never matched")
	}
	got := db.putMetaDocs[0]
	if got.ID != obfID {
		t.Errorf("tombstoned wrong document: got %q, want %q", got.ID, obfID)
	}
	if !got.DeletedApp {
		t.Error("expected app-level deletion marker (deleted: true)")
	}
	if got.Deleted {
		t.Error("tombstone must not use CouchDB _deleted — path info would be lost")
	}
}
