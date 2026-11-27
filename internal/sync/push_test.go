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

func TestPush_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push empty dir: %v", err)
	}

	if db.bulkDocsCalled {
		t.Error("BulkDocs should not be called for empty dir")
	}
	if db.putMetaCalled {
		t.Error("PutMeta should not be called for empty dir")
	}
}

func TestPush_OneFile(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	content := []byte("hello obgo")
	absPath := filepath.Join(tmpDir, "note.md")
	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if !db.bulkDocsCalled {
		t.Error("BulkDocs should be called")
	}
	if !db.putMetaCalled {
		t.Error("PutMeta should be called")
	}
	if len(db.putMetaDocs) != 1 {
		t.Fatalf("expected 1 PutMeta call, got %d", len(db.putMetaDocs))
	}
	meta := db.putMetaDocs[0]
	if meta.Path != "note.md" {
		t.Errorf("meta path: got %q, want %q", meta.Path, "note.md")
	}
	if meta.Size != int64(len(content)) {
		t.Errorf("meta size: got %d, want %d", meta.Size, len(content))
	}
	if len(meta.Children) == 0 {
		t.Error("meta should have at least one chunk ID")
	}
}

func TestPush_SubdirFile(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	subDir := filepath.Join(tmpDir, "notes", "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "deep.md"), []byte("deep content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(db.putMetaDocs) != 1 {
		t.Fatalf("expected 1 PutMeta call, got %d", len(db.putMetaDocs))
	}
	if db.putMetaDocs[0].Path != "notes/subdir/deep.md" {
		t.Errorf("path: got %q, want %q", db.putMetaDocs[0].Path, "notes/subdir/deep.md")
	}
}

func TestPush_ExplicitHiddenFolderFilterStillPushes(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	hiddenDir := filepath.Join(tmpDir, ".obsidian")
	if err := os.Mkdir(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "app.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Explicitly requesting a hidden folder should push its contents even
	// with OBGO_SYNC_HIDDEN off, matching the same exemption the walk
	// already grants walkRoot for directory pruning.
	if err := svc.Push(context.Background(), ".obsidian/"); err != nil {
		t.Fatalf("Push .obsidian/: %v", err)
	}

	if len(db.putMetaDocs) != 1 {
		t.Fatalf("expected 1 PutMeta call, got %d", len(db.putMetaDocs))
	}
	if db.putMetaDocs[0].Path != ".obsidian/app.json" {
		t.Errorf("path: got %q, want %q", db.putMetaDocs[0].Path, ".obsidian/app.json")
	}
}

func TestPush_ExplicitHiddenFolderFilterStillIgnoresGitDir(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Even an explicit ".git/" filter must never push — protected names are
	// never overridable.
	if err := svc.Push(context.Background(), ".git/"); err != nil {
		t.Fatalf("Push .git/: %v", err)
	}

	if len(db.putMetaDocs) != 0 {
		t.Fatalf("expected .git to remain protected, got %d PutMeta calls", len(db.putMetaDocs))
	}
}

func TestPush_HiddenDirIgnoredByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	hiddenDir := filepath.Join(tmpDir, ".agents")
	if err := os.Mkdir(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "skill.md"), []byte("hidden content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(db.putMetaDocs) != 0 {
		t.Fatalf("expected hidden file to be ignored by default, got %d PutMeta calls", len(db.putMetaDocs))
	}
}

func TestPush_SyncHiddenPushesHiddenDir(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)
	svc.SetSyncHidden(true)

	hiddenDir := filepath.Join(tmpDir, ".agents")
	if err := os.Mkdir(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "skill.md"), []byte("hidden content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(db.putMetaDocs) != 1 {
		t.Fatalf("expected 1 PutMeta call, got %d", len(db.putMetaDocs))
	}
	if db.putMetaDocs[0].Path != ".agents/skill.md" {
		t.Errorf("path: got %q, want %q", db.putMetaDocs[0].Path, ".agents/skill.md")
	}
}

func TestPush_SyncHiddenStillIgnoresGitDir(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)
	svc.SetSyncHidden(true)

	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(db.putMetaDocs) != 0 {
		t.Fatalf("expected .git file to be ignored, got %d PutMeta calls", len(db.putMetaDocs))
	}
}

func TestPush_HiddenRemoteDocIgnoredByDefaultDuringReconcile(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	db.metaDocs = []couchdb.MetaDoc{
		{ID: ".agents/skill.md", Type: "plain", Path: ".agents/skill.md", Rev: "1-hidden"},
	}

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(db.putMetaDocs) != 0 {
		t.Fatalf("expected hidden remote doc to be ignored, got %d PutMeta calls", len(db.putMetaDocs))
	}
}

func TestPush_E2EE(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("secret-password")
	svc := syncsvc.New(db, cr, tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "secret.md"), []byte("private data"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push E2EE: %v", err)
	}

	if !db.putMetaCalled {
		t.Error("PutMeta should be called")
	}
	if !db.putMetaDocs[0].Encrypted {
		t.Error("meta should be marked encrypted")
	}

	// Verify salt was stored in _local.
	if _, ok := db.localDocs["obsidian_livesync_sync_parameters"]; !ok {
		t.Error("salt should be stored in _local/obsidian_livesync_sync_parameters")
	}
}

func TestPush_SingleFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "wanted.md"), []byte("content A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "other.md"), []byte("content B"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Push(context.Background(), "wanted.md"); err != nil {
		t.Fatalf("Push single file: %v", err)
	}

	if len(db.putMetaDocs) != 1 {
		t.Fatalf("expected 1 PutMeta call, got %d", len(db.putMetaDocs))
	}
	if db.putMetaDocs[0].Path != "wanted.md" {
		t.Errorf("pushed wrong file: got %q, want %q", db.putMetaDocs[0].Path, "wanted.md")
	}
}

func TestPush_SingleHiddenFileIgnoredByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	hiddenDir := filepath.Join(tmpDir, ".agents")
	if err := os.Mkdir(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "skill.md"), []byte("hidden content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Push(context.Background(), ".agents/skill.md"); err != nil {
		t.Fatalf("Push single hidden file: %v", err)
	}

	if len(db.putMetaDocs) != 0 {
		t.Fatalf("expected hidden file to be ignored by default, got %d PutMeta calls", len(db.putMetaDocs))
	}
}

func TestPush_SyncHiddenPushesSingleHiddenFile(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)
	svc.SetSyncHidden(true)

	hiddenDir := filepath.Join(tmpDir, ".agents")
	if err := os.Mkdir(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "skill.md"), []byte("hidden content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Push(context.Background(), ".agents/skill.md"); err != nil {
		t.Fatalf("Push single hidden file: %v", err)
	}

	if len(db.putMetaDocs) != 1 {
		t.Fatalf("expected 1 PutMeta call, got %d", len(db.putMetaDocs))
	}
	if db.putMetaDocs[0].Path != ".agents/skill.md" {
		t.Errorf("path: got %q, want %q", db.putMetaDocs[0].Path, ".agents/skill.md")
	}
}

func TestPush_TombstonesRemoteDocForMissingLocalFile(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	// Remote has a doc that no longer exists locally.
	db.metaDocs = []couchdb.MetaDoc{
		{ID: "gone.md", Type: "plain", Path: "gone.md", Rev: "1-aaa"},
	}
	// Local vault is empty.

	if err := svc.Push(context.Background(), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if len(db.putMetaDocs) != 1 {
		t.Fatalf("expected 1 PutMeta (tombstone) call, got %d", len(db.putMetaDocs))
	}
	if !db.putMetaDocs[0].DeletedApp {
		t.Error("expected remote doc to carry app-level deletion marker (deleted: true)")
	}
	if db.putMetaDocs[0].Deleted {
		t.Error("tombstone must not use CouchDB _deleted — path info would be lost")
	}
	if db.putMetaDocs[0].Path != "gone.md" {
		t.Errorf("tombstoned wrong doc: got %q, want %q", db.putMetaDocs[0].Path, "gone.md")
	}
}

func TestPush_SingleFileTombstonesWhenMissing(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	// Remote has the doc; local file was deleted.
	db.metaDocs = []couchdb.MetaDoc{
		{ID: "note.md", Type: "plain", Path: "note.md", Rev: "1-bbb"},
	}

	if err := svc.Push(context.Background(), "note.md"); err != nil {
		t.Fatalf("Push single missing file: %v", err)
	}

	if len(db.putMetaDocs) != 1 {
		t.Fatalf("expected 1 PutMeta (tombstone) call, got %d", len(db.putMetaDocs))
	}
	if !db.putMetaDocs[0].DeletedApp {
		t.Error("expected remote doc to carry app-level deletion marker (deleted: true)")
	}
	if db.putMetaDocs[0].Deleted {
		t.Error("tombstone must not use CouchDB _deleted — path info would be lost")
	}
}

func TestPush_FolderPath(t *testing.T) {
	tmpDir := t.TempDir()
	db := newMockClient()
	cr := crypto.New("")
	svc := syncsvc.New(db, cr, tmpDir)

	notesDir := filepath.Join(tmpDir, "notes")
	projectsDir := filepath.Join(tmpDir, "projects")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "a.md"), []byte("note a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "b.md"), []byte("note b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectsDir, "x.md"), []byte("project x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Push(context.Background(), "notes/"); err != nil {
		t.Fatalf("Push folder: %v", err)
	}

	if len(db.putMetaDocs) != 2 {
		t.Fatalf("expected 2 PutMeta calls (notes only), got %d", len(db.putMetaDocs))
	}
	for _, m := range db.putMetaDocs {
		if m.Path != "notes/a.md" && m.Path != "notes/b.md" {
			t.Errorf("unexpected file pushed: %q", m.Path)
		}
	}
}
