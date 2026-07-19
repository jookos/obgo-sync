package sync_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/jookos/obgo-sync/internal/couchdb"
	"github.com/jookos/obgo-sync/internal/crypto"
	syncsvc "github.com/jookos/obgo-sync/internal/sync"
)

// Push decrypts the paths of the pre-loaded remote docs in place, and PutMeta
// serialises whatever struct it is handed. Tombstoning that same struct would
// therefore replace the encrypted path blob with the cleartext path, leaking
// the path of every deleted document on an obfuscated vault.
func TestPush_TombstoneKeepsObfuscatedPath(t *testing.T) {
	const password = "test-obfuscate-pass"
	const saltStr = "test-push-salt-32-bytes-padding!"

	cr := crypto.New(password)
	cr.SetSalt([]byte(saltStr))

	const realPath = "notes/gone.md"
	encPath, err := cr.EncryptPath(realPath, 1000, 2000, 7, []string{"h:+chunkA"})
	if err != nil {
		t.Fatalf("EncryptPath: %v", err)
	}

	tmpDir := t.TempDir() // empty vault: the remote doc must be tombstoned
	db := newMockClient()
	db.localDocs["obsidian_livesync_sync_parameters"] = map[string]interface{}{
		"pbkdf2salt": base64.StdEncoding.EncodeToString([]byte(saltStr)),
	}
	db.metaDocs = []couchdb.MetaDoc{{
		ID:        cr.ObfuscateDocID(realPath),
		Type:      "plain",
		Path:      encPath,
		Rev:       "1-aaa",
		Children:  []string{"h:+chunkA"},
		Encrypted: true,
	}}

	svc := syncsvc.New(db, cr, tmpDir)
	svc.SetObfuscationMode("on")

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(db.putMetaDocs) != 1 {
		t.Fatalf("expected 1 tombstone, got %d", len(db.putMetaDocs))
	}
	got := db.putMetaDocs[0]
	if !got.DeletedApp {
		t.Error("expected app-level deletion marker")
	}
	if got.Path == realPath {
		t.Fatalf("tombstone leaked the cleartext path %q — path obfuscation defeated", realPath)
	}
	if !strings.HasPrefix(got.Path, "/\\:") {
		t.Errorf("tombstone should preserve the encrypted path blob, got %q", got.Path)
	}
}
