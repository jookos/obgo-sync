package couchdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrNotFound is returned when a requested document does not exist (HTTP 404).
var ErrNotFound = errors.New("not found")

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

// dbURL builds a URL for a path under the database.
func (c *HTTPClient) dbURL(parts ...string) string {
	segments := append([]string{c.dbName}, parts...)
	return c.baseURL.String() + "/" + strings.Join(segments, "/")
}

// do executes an HTTP request with basic auth and returns the response body.
// The caller must close the body. Returns ErrNotFound on HTTP 404.
func (c *HTTPClient) do(req *http.Request) (*http.Response, error) {
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("couchdb: HTTP %d: %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

// isConflict reports whether err is a CouchDB HTTP 409 Conflict error.
func isConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "HTTP 409")
}

// getJSON performs a GET request and decodes the JSON response into dst.
func (c *HTTPClient) getJSON(ctx context.Context, rawURL string, dst interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(dst)
}

// putJSON performs a PUT request with a JSON body and decodes the response.
func (c *HTTPClient) putJSON(ctx context.Context, rawURL string, body interface{}, dst interface{}) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// postJSON performs a POST request with a JSON body and decodes the response.
func (c *HTTPClient) postJSON(ctx context.Context, rawURL string, body interface{}, dst interface{}) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// AllMetaDocs fetches all non-chunk documents from CouchDB and returns those
// with type "plain" or "newnote" that are not deleted.
func (c *HTTPClient) AllMetaDocs(ctx context.Context) ([]MetaDoc, error) {
	rawURL := c.dbURL("_all_docs") + "?include_docs=true"
	var result struct {
		Rows []struct {
			Doc *MetaDoc `json:"doc"`
		} `json:"rows"`
	}
	if err := c.getJSON(ctx, rawURL, &result); err != nil {
		return nil, fmt.Errorf("AllMetaDocs: %w", err)
	}

	var docs []MetaDoc
	for _, row := range result.Rows {
		if row.Doc == nil {
			continue
		}
		if row.Doc.Deleted {
			continue
		}
		if row.Doc.Type != "plain" && row.Doc.Type != "newnote" {
			continue
		}
		docs = append(docs, *row.Doc)
	}
	return docs, nil
}

// GetMeta fetches a single meta document by its encoded document ID.
func (c *HTTPClient) GetMeta(ctx context.Context, id string) (*MetaDoc, error) {
	rawURL := c.dbURL(url.PathEscape(id))
	var doc MetaDoc
	if err := c.getJSON(ctx, rawURL, &doc); err != nil {
		return nil, fmt.Errorf("GetMeta %q: %w", id, err)
	}
	return &doc, nil
}

// PutMeta creates or updates a meta document in CouchDB.
// On 409 Conflict it fetches the current rev and retries once.
// Returns the new revision string on success.
func (c *HTTPClient) PutMeta(ctx context.Context, doc *MetaDoc) (string, error) {
	rawURL := c.dbURL(url.PathEscape(doc.ID))
	var result struct {
		OK  bool   `json:"ok"`
		Rev string `json:"rev"`
	}
	err := c.putJSON(ctx, rawURL, doc, &result)
	if err != nil {
		// On conflict: fetch current rev, set it and retry once.
		if isConflict(err) {
			existing, ferr := c.GetMeta(ctx, doc.ID)
			if ferr != nil {
				return "", fmt.Errorf("PutMeta conflict, fetch rev: %w", ferr)
			}
			doc.Rev = existing.Rev
			if rerr := c.putJSON(ctx, rawURL, doc, &result); rerr != nil {
				return "", fmt.Errorf("PutMeta retry: %w", rerr)
			}
			return result.Rev, nil
		}
		return "", fmt.Errorf("PutMeta %q: %w", doc.ID, err)
	}
	return result.Rev, nil
}

// GetChunk fetches a single chunk document by ID.
func (c *HTTPClient) GetChunk(ctx context.Context, id string) (*ChunkDoc, error) {
	rawURL := c.dbURL(url.PathEscape(id))
	var doc ChunkDoc
	if err := c.getJSON(ctx, rawURL, &doc); err != nil {
		return nil, fmt.Errorf("GetChunk %q: %w", id, err)
	}
	return &doc, nil
}

// PutChunk creates or updates a chunk document in CouchDB.
// On 409 Conflict it treats the chunk as already stored (content-addressed) and
// returns the existing rev without error.
func (c *HTTPClient) PutChunk(ctx context.Context, doc *ChunkDoc) (string, error) {
	rawURL := c.dbURL(url.PathEscape(doc.ID))
	var result struct {
		OK  bool   `json:"ok"`
		Rev string `json:"rev"`
	}
	err := c.putJSON(ctx, rawURL, doc, &result)
	if err != nil {
		if isConflict(err) {
			// Content-addressed: already exists, fetch its rev.
			existing, ferr := c.GetChunk(ctx, doc.ID)
			if ferr != nil {
				return "", fmt.Errorf("PutChunk conflict, fetch rev: %w", ferr)
			}
			return existing.Rev, nil
		}
		return "", fmt.Errorf("PutChunk %q: %w", doc.ID, err)
	}
	return result.Rev, nil
}

// BulkGet fetches multiple chunk documents in one request using _bulk_get.
func (c *HTTPClient) BulkGet(ctx context.Context, ids []string) ([]ChunkDoc, error) {
	type bulkGetDoc struct {
		ID string `json:"id"`
	}
	type bulkGetRequest struct {
		Docs []bulkGetDoc `json:"docs"`
	}
	reqDocs := make([]bulkGetDoc, len(ids))
	for i, id := range ids {
		reqDocs[i] = bulkGetDoc{ID: id}
	}

	var result struct {
		Results []struct {
			Docs []struct {
				OK    *ChunkDoc       `json:"ok"`
				Error json.RawMessage `json:"error"`
			} `json:"docs"`
		} `json:"results"`
	}

	if err := c.postJSON(ctx, c.dbURL("_bulk_get"), bulkGetRequest{Docs: reqDocs}, &result); err != nil {
		return nil, fmt.Errorf("BulkGet: %w", err)
	}

	var chunks []ChunkDoc
	for _, r := range result.Results {
		for _, d := range r.Docs {
			if d.OK != nil {
				chunks = append(chunks, *d.OK)
			}
		}
	}
	return chunks, nil
}

// BulkDocs uploads multiple documents in one request using _bulk_docs.
// Returns a combined error if any document failed.
func (c *HTTPClient) BulkDocs(ctx context.Context, docs []interface{}) error {
	if len(docs) == 0 {
		return nil
	}
	body := map[string]interface{}{"docs": docs}
	var results []struct {
		ID    string `json:"id"`
		Error string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := c.postJSON(ctx, c.dbURL("_bulk_docs"), body, &results); err != nil {
		return fmt.Errorf("BulkDocs: %w", err)
	}
	var errs []string
	for _, r := range results {
		if r.Error != "" && r.Error != "conflict" {
			errs = append(errs, fmt.Sprintf("%s: %s (%s)", r.ID, r.Error, r.Reason))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("BulkDocs errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (c *HTTPClient) Changes(ctx context.Context, since string) (<-chan ChangeEvent, error) {
	return nil, errors.New("Changes: not yet implemented")
}

// GetLocal fetches a _local document by its short ID (without the "_local/" prefix).
func (c *HTTPClient) GetLocal(ctx context.Context, id string) (map[string]interface{}, error) {
	rawURL := c.dbURL("_local", id)
	var doc map[string]interface{}
	if err := c.getJSON(ctx, rawURL, &doc); err != nil {
		return nil, fmt.Errorf("GetLocal %q: %w", id, err)
	}
	return doc, nil
}

// PutLocal creates or updates a _local document in CouchDB.
// On 409 Conflict it fetches the current rev, sets it in the doc map and retries once.
func (c *HTTPClient) PutLocal(ctx context.Context, id string, doc map[string]interface{}) error {
	rawURL := c.dbURL("_local", id)
	err := c.putJSON(ctx, rawURL, doc, nil)
	if err != nil {
		if isConflict(err) {
			existing, ferr := c.GetLocal(ctx, id)
			if ferr != nil {
				return fmt.Errorf("PutLocal conflict, fetch rev: %w", ferr)
			}
			if rev, ok := existing["_rev"].(string); ok {
				doc["_rev"] = rev
			}
			if rerr := c.putJSON(ctx, rawURL, doc, nil); rerr != nil {
				return fmt.Errorf("PutLocal retry: %w", rerr)
			}
			return nil
		}
		return fmt.Errorf("PutLocal %q: %w", id, err)
	}
	return nil
}

// ServerInfo fetches the database info document.
func (c *HTTPClient) ServerInfo(ctx context.Context) (map[string]interface{}, error) {
	rawURL := c.dbURL()
	var info map[string]interface{}
	if err := c.getJSON(ctx, rawURL, &info); err != nil {
		return nil, fmt.Errorf("ServerInfo: %w", err)
	}
	return info, nil
}
