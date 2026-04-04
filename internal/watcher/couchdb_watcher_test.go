package watcher_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jookos/obgo/internal/couchdb"
	"github.com/jookos/obgo/internal/watcher"
)

// mockChangesClient is a couchdb.Client stub that returns a pre-loaded changes channel.
type mockChangesClient struct {
	ch <-chan couchdb.ChangeEvent
}

func (m *mockChangesClient) AllMetaDocs(ctx context.Context) ([]couchdb.MetaDoc, error) {
	return nil, nil
}
func (m *mockChangesClient) GetMeta(ctx context.Context, id string) (*couchdb.MetaDoc, error) {
	return nil, couchdb.ErrNotFound
}
func (m *mockChangesClient) PutMeta(ctx context.Context, doc *couchdb.MetaDoc) (string, error) {
	return "", nil
}
func (m *mockChangesClient) GetChunk(ctx context.Context, id string) (*couchdb.ChunkDoc, error) {
	return nil, couchdb.ErrNotFound
}
func (m *mockChangesClient) PutChunk(ctx context.Context, doc *couchdb.ChunkDoc) (string, error) {
	return "", nil
}
func (m *mockChangesClient) BulkGet(ctx context.Context, ids []string) ([]couchdb.ChunkDoc, error) {
	return nil, nil
}
func (m *mockChangesClient) BulkDocs(ctx context.Context, docs []interface{}) error { return nil }
func (m *mockChangesClient) Changes(ctx context.Context, since string) (<-chan couchdb.ChangeEvent, error) {
	return m.ch, nil
}
func (m *mockChangesClient) GetLocal(ctx context.Context, id string) (map[string]interface{}, error) {
	return nil, couchdb.ErrNotFound
}
func (m *mockChangesClient) PutLocal(ctx context.Context, id string, doc map[string]interface{}) error {
	return nil
}
func (m *mockChangesClient) GetMetaAtRev(ctx context.Context, id, rev string) (*couchdb.MetaDoc, error) {
	return nil, couchdb.ErrNotFound
}
func (m *mockChangesClient) DeleteRevision(ctx context.Context, id, rev string) error { return nil }
func (m *mockChangesClient) ServerInfo(ctx context.Context) (map[string]interface{}, error) {
	return nil, nil
}

func TestRemoteWatcher_ProcessesEvents(t *testing.T) {
	dir := t.TempDir()

	events := []couchdb.ChangeEvent{
		{Seq: "1", ID: "file1", Doc: &couchdb.MetaDoc{ID: "file1", Path: "notes/a.md"}},
		{Seq: "2", ID: "file2", Doc: &couchdb.MetaDoc{ID: "file2", Path: "notes/b.md"}},
		{Seq: "3", ID: "file3", Doc: &couchdb.MetaDoc{ID: "file3", Path: "notes/c.md"}},
	}

	ch := make(chan couchdb.ChangeEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	client := &mockChangesClient{ch: ch}

	var mu sync.Mutex
	var received []couchdb.ChangeEvent

	rw := watcher.NewRemoteWatcher(client, dir, func(ctx context.Context, event couchdb.ChangeEvent) error {
		mu.Lock()
		received = append(received, event)
		mu.Unlock()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = rw.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != len(events) {
		t.Fatalf("expected %d events, got %d", len(events), len(received))
	}
	for i, e := range events {
		if received[i].ID != e.ID {
			t.Errorf("event[%d]: expected ID %q, got %q", i, e.ID, received[i].ID)
		}
	}
}

func TestRemoteWatcher_PersistsSeq(t *testing.T) {
	dir := t.TempDir()

	events := []couchdb.ChangeEvent{
		{Seq: "10", ID: "doc1", Doc: &couchdb.MetaDoc{ID: "doc1", Path: "x.md"}},
		{Seq: "20", ID: "doc2", Doc: &couchdb.MetaDoc{ID: "doc2", Path: "y.md"}},
	}

	ch := make(chan couchdb.ChangeEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	client := &mockChangesClient{ch: ch}

	rw := watcher.NewRemoteWatcher(client, dir, func(ctx context.Context, event couchdb.ChangeEvent) error {
		return nil
	})

	ctx := context.Background()
	_ = rw.Run(ctx)

	seqFile := filepath.Join(dir, ".obgo_seq")
	data, err := os.ReadFile(seqFile)
	if err != nil {
		t.Fatalf("expected .obgo_seq to exist: %v", err)
	}
	if string(data) != "20" {
		t.Errorf("expected last seq %q, got %q", "20", string(data))
	}
}

func TestRemoteWatcher_LoadsSeqOnStart(t *testing.T) {
	dir := t.TempDir()

	// Pre-write a seq file.
	seqFile := filepath.Join(dir, ".obgo_seq")
	if err := os.WriteFile(seqFile, []byte("42"), 0o644); err != nil {
		t.Fatal(err)
	}

	var receivedSince string
	ch := make(chan couchdb.ChangeEvent)
	close(ch)

	client := &mockChangesClient{}
	// Override Changes to capture the since parameter.
	capturingClient := &captureSinceClient{
		inner: client,
		ch:    ch,
		onChanges: func(since string) {
			receivedSince = since
		},
	}

	rw := watcher.NewRemoteWatcher(capturingClient, dir, func(ctx context.Context, event couchdb.ChangeEvent) error {
		return nil
	})

	ctx := context.Background()
	_ = rw.Run(ctx)

	if receivedSince != "42" {
		t.Errorf("expected since=%q to be passed to Changes, got %q", "42", receivedSince)
	}
}

func TestRemoteWatcher_EventCallbackError_Continues(t *testing.T) {
	dir := t.TempDir()

	events := []couchdb.ChangeEvent{
		{Seq: "1", ID: "err-doc"},
		{Seq: "2", ID: "ok-doc"},
	}

	ch := make(chan couchdb.ChangeEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	client := &mockChangesClient{ch: ch}
	processed := 0

	rw := watcher.NewRemoteWatcher(client, dir, func(ctx context.Context, event couchdb.ChangeEvent) error {
		processed++
		if event.ID == "err-doc" {
			return fmt.Errorf("simulated error")
		}
		return nil
	})

	ctx := context.Background()
	_ = rw.Run(ctx)

	if processed != 2 {
		t.Errorf("expected both events processed, got %d", processed)
	}
}

// captureSinceClient wraps mockChangesClient and captures the since argument.
type captureSinceClient struct {
	inner     *mockChangesClient
	ch        <-chan couchdb.ChangeEvent
	onChanges func(since string)
}

func (c *captureSinceClient) AllMetaDocs(ctx context.Context) ([]couchdb.MetaDoc, error) {
	return c.inner.AllMetaDocs(ctx)
}
func (c *captureSinceClient) GetMeta(ctx context.Context, id string) (*couchdb.MetaDoc, error) {
	return c.inner.GetMeta(ctx, id)
}
func (c *captureSinceClient) PutMeta(ctx context.Context, doc *couchdb.MetaDoc) (string, error) {
	return c.inner.PutMeta(ctx, doc)
}
func (c *captureSinceClient) GetChunk(ctx context.Context, id string) (*couchdb.ChunkDoc, error) {
	return c.inner.GetChunk(ctx, id)
}
func (c *captureSinceClient) PutChunk(ctx context.Context, doc *couchdb.ChunkDoc) (string, error) {
	return c.inner.PutChunk(ctx, doc)
}
func (c *captureSinceClient) BulkGet(ctx context.Context, ids []string) ([]couchdb.ChunkDoc, error) {
	return c.inner.BulkGet(ctx, ids)
}
func (c *captureSinceClient) BulkDocs(ctx context.Context, docs []interface{}) error {
	return c.inner.BulkDocs(ctx, docs)
}
func (c *captureSinceClient) Changes(ctx context.Context, since string) (<-chan couchdb.ChangeEvent, error) {
	c.onChanges(since)
	return c.ch, nil
}
func (c *captureSinceClient) GetLocal(ctx context.Context, id string) (map[string]interface{}, error) {
	return c.inner.GetLocal(ctx, id)
}
func (c *captureSinceClient) PutLocal(ctx context.Context, id string, doc map[string]interface{}) error {
	return c.inner.PutLocal(ctx, id, doc)
}
func (c *captureSinceClient) GetMetaAtRev(ctx context.Context, id, rev string) (*couchdb.MetaDoc, error) {
	return c.inner.GetMetaAtRev(ctx, id, rev)
}
func (c *captureSinceClient) DeleteRevision(ctx context.Context, id, rev string) error {
	return c.inner.DeleteRevision(ctx, id, rev)
}
func (c *captureSinceClient) ServerInfo(ctx context.Context) (map[string]interface{}, error) {
	return c.inner.ServerInfo(ctx)
}
