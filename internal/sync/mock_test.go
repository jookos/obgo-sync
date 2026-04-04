package sync_test

import (
	"context"

	"github.com/jookos/obgo-sync/internal/couchdb"
)

// mockClient implements couchdb.Client for testing.
type mockClient struct {
	metaDocs   []couchdb.MetaDoc
	chunkDocs  map[string]couchdb.ChunkDoc
	localDocs  map[string]map[string]interface{}
	bulkDocsCalled bool
	putMetaCalled  bool
	putMetaDocs    []*couchdb.MetaDoc
	bulkDocsArg    []interface{}
}

func newMockClient() *mockClient {
	return &mockClient{
		chunkDocs: make(map[string]couchdb.ChunkDoc),
		localDocs: make(map[string]map[string]interface{}),
	}
}

func (m *mockClient) AllMetaDocs(ctx context.Context) ([]couchdb.MetaDoc, error) {
	return m.metaDocs, nil
}

func (m *mockClient) GetMeta(ctx context.Context, id string) (*couchdb.MetaDoc, error) {
	for _, d := range m.metaDocs {
		if d.ID == id {
			cp := d
			return &cp, nil
		}
	}
	return nil, couchdb.ErrNotFound
}

func (m *mockClient) PutMeta(ctx context.Context, doc *couchdb.MetaDoc) (string, error) {
	m.putMetaCalled = true
	m.putMetaDocs = append(m.putMetaDocs, doc)
	return "1-abc", nil
}

func (m *mockClient) GetChunk(ctx context.Context, id string) (*couchdb.ChunkDoc, error) {
	if d, ok := m.chunkDocs[id]; ok {
		return &d, nil
	}
	return nil, couchdb.ErrNotFound
}

func (m *mockClient) PutChunk(ctx context.Context, doc *couchdb.ChunkDoc) (string, error) {
	m.chunkDocs[doc.ID] = *doc
	return "1-abc", nil
}

func (m *mockClient) BulkGet(ctx context.Context, ids []string) ([]couchdb.ChunkDoc, error) {
	var out []couchdb.ChunkDoc
	for _, id := range ids {
		if d, ok := m.chunkDocs[id]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func (m *mockClient) BulkDocs(ctx context.Context, docs []interface{}) error {
	m.bulkDocsCalled = true
	m.bulkDocsArg = docs
	// Store chunks so they can be retrieved later.
	for _, d := range docs {
		if cd, ok := d.(*couchdb.ChunkDoc); ok {
			m.chunkDocs[cd.ID] = *cd
		}
	}
	return nil
}

func (m *mockClient) Changes(ctx context.Context, since string) (<-chan couchdb.ChangeEvent, error) {
	ch := make(chan couchdb.ChangeEvent)
	close(ch)
	return ch, nil
}

func (m *mockClient) GetLocal(ctx context.Context, id string) (map[string]interface{}, error) {
	if d, ok := m.localDocs[id]; ok {
		return d, nil
	}
	return nil, couchdb.ErrNotFound
}

func (m *mockClient) PutLocal(ctx context.Context, id string, doc map[string]interface{}) error {
	m.localDocs[id] = doc
	return nil
}

func (m *mockClient) GetMetaAtRev(ctx context.Context, id, rev string) (*couchdb.MetaDoc, error) {
	return nil, couchdb.ErrNotFound
}

func (m *mockClient) DeleteRevision(ctx context.Context, id, rev string) error {
	return nil
}

func (m *mockClient) ServerInfo(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{"couchdb": "Welcome"}, nil
}
