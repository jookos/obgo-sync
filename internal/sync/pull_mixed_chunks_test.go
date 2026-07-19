package sync_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/jookos/obgo-sync/internal/couchdb"
	"github.com/jookos/obgo-sync/internal/crypto"
	syncsvc "github.com/jookos/obgo-sync/internal/sync"
)

// Enabling E2EE on an existing vault does not retroactively re-encrypt old
// chunks unless a full rebuild is run, so a real vault holds a mix of
// encrypted and plaintext chunks. Classifying them by ciphertext prefix is
// unreliable in both directions: a plaintext chunk has no prefix, and one may
// legitimately begin with "%" (the V1 marker) — e.g. a Templater directive —
// which misroutes it into the decryptor and fails the whole pull. The
// per-chunk e_ flag is authoritative and is what the official client uses.
func TestPull_MixedEncryptedAndPlaintextChunks(t *testing.T) {
	const password = "test-mixed-pass"
	const saltStr = "test-pull-salt-32-bytes-padding!"

	cr := crypto.New(password)
	cr.SetSalt([]byte(saltStr))

	encData, err := cr.EncryptContent([]byte("encrypted part. "))
	if err != nil {
		t.Fatalf("EncryptContent: %v", err)
	}
	// Plaintext chunk that starts with the V1 ciphertext marker.
	const plainBody = "% tp.system.prompt() plaintext part"

	tmpDir := t.TempDir()
	db := newMockClient()
	db.localDocs["obsidian_livesync_sync_parameters"] = map[string]interface{}{
		"pbkdf2salt": base64.StdEncoding.EncodeToString([]byte(saltStr)),
	}
	db.chunkDocs["h:+enc"] = couchdb.ChunkDoc{ID: "h:+enc", Data: encData, Type: "leaf", Encrypted: true}
	db.chunkDocs["h:+plain"] = couchdb.ChunkDoc{ID: "h:+plain", Data: plainBody, Type: "leaf", Encrypted: false}
	db.metaDocs = []couchdb.MetaDoc{{
		ID:       "mixed.md",
		Type:     "plain",
		Path:     "mixed.md",
		Children: []string{"h:+enc", "h:+plain"},
	}}

	svc := syncsvc.New(db, cr, tmpDir)
	if err := svc.Pull(context.Background(), ""); err != nil {
		t.Fatalf("Pull with mixed chunks: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(tmpDir, "mixed.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if want := "encrypted part. " + plainBody; string(got) != want {
		t.Errorf("content mismatch:\n got %q\nwant %q", got, want)
	}
}
