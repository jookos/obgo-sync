package couchdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrNotImplemented is returned by stub methods that are not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// Client abstracts all CouchDB HTTP operations needed by obgo-live.
type Client interface {
	AllMetaDocs(ctx context.Context) ([]MetaDoc, error)
	GetMeta(ctx context.Context, id string) (*MetaDoc, error)
	PutMeta(ctx context.Context, doc *MetaDoc) (string, error)
	GetChunk(ctx context.Context, id string) (*ChunkDoc, error)
	PutChunk(ctx context.Context, doc *ChunkDoc) (string, error)
	BulkGet(ctx context.Context, ids []string) ([]ChunkDoc, error)
	BulkDocs(ctx context.Context, docs []interface{}) error
	Changes(ctx context.Context, since string) (<-chan ChangeEvent, error)
	GetLocal(ctx context.Context, id string) (map[string]interface{}, error)
	PutLocal(ctx context.Context, id string, doc map[string]interface{}) error
	ServerInfo(ctx context.Context) (map[string]interface{}, error)
}

// HTTPClient implements Client using the CouchDB HTTP API.
type HTTPClient struct {
	baseURL    *url.URL
	dbName     string
	httpClient *http.Client
	username   string
	password   string
}

// New parses rawURL and returns an HTTPClient.
// The URL must include the database name as the path component.
// Credentials may be embedded in the URL (user:password@host).
func New(rawURL string) (*HTTPClient, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid CouchDB URL: %w", err)
	}

	// Extract db name from path (last path segment).
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return nil, errors.New("CouchDB URL must include a database name in the path")
	}

	var username, password string
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}

	// Build the base URL without the db path so we can construct request URLs.
	base := &url.URL{
		Scheme: u.Scheme,
		Host:   u.Host,
	}

	return &HTTPClient{
		baseURL:    base,
		dbName:     path,
		httpClient: &http.Client{},
		username:   username,
		password:   password,
	}, nil
}

func (c *HTTPClient) AllMetaDocs(ctx context.Context) ([]MetaDoc, error) {
	return nil, ErrNotImplemented
}

func (c *HTTPClient) GetMeta(ctx context.Context, id string) (*MetaDoc, error) {
	return nil, ErrNotImplemented
}

func (c *HTTPClient) PutMeta(ctx context.Context, doc *MetaDoc) (string, error) {
	return "", ErrNotImplemented
}

func (c *HTTPClient) GetChunk(ctx context.Context, id string) (*ChunkDoc, error) {
	return nil, ErrNotImplemented
}

func (c *HTTPClient) PutChunk(ctx context.Context, doc *ChunkDoc) (string, error) {
	return "", ErrNotImplemented
}

func (c *HTTPClient) BulkGet(ctx context.Context, ids []string) ([]ChunkDoc, error) {
	return nil, ErrNotImplemented
}

func (c *HTTPClient) BulkDocs(ctx context.Context, docs []interface{}) error {
	return ErrNotImplemented
}

func (c *HTTPClient) Changes(ctx context.Context, since string) (<-chan ChangeEvent, error) {
	return nil, ErrNotImplemented
}

func (c *HTTPClient) GetLocal(ctx context.Context, id string) (map[string]interface{}, error) {
	return nil, ErrNotImplemented
}

func (c *HTTPClient) PutLocal(ctx context.Context, id string, doc map[string]interface{}) error {
	return ErrNotImplemented
}

func (c *HTTPClient) ServerInfo(ctx context.Context) (map[string]interface{}, error) {
	return nil, ErrNotImplemented
}
