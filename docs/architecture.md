# Architecture

## Package dependency diagram

```
cmd/obgo
  |
  +---> internal/config         (load env vars)
  |
  +---> internal/couchdb        (HTTP client)
  |
  +---> internal/crypto         (E2EE encrypt/decrypt)
  |
  +---> internal/sync           (pull / push / watch orchestration)
           |
           +---> internal/couchdb   (via Client interface)
           +---> internal/crypto    (via *crypto.Service)
           +---> internal/watcher   (RemoteWatcher, LocalWatcher)
           +---> lib/livesync       (EncodeDocID, Split)
```

`internal/watcher` depends only on `internal/couchdb` (the `Client` interface and `ChangeEvent` type).  
`lib/livesync` has no internal dependencies.

---

## Package responsibilities

### `cmd/obgo`

CLI entry point built with [cobra](https://github.com/spf13/cobra). Registers `pull` and `push` sub-commands (both support `--watch`/`-w`). Loads the `.env` file, wires together the config, CouchDB client, crypto service, and sync service, then dispatches to `svc.Pull`, `svc.Push`, and optionally `svc.Watch`. Installs a signal handler (SIGINT/SIGTERM) for graceful shutdown via context cancellation.

### `internal/config`

Reads the three required environment variables (`COUCHDB_URL`, `E2EE_PASSWORD`, `OBGO_DATA`) and returns a validated `Config` struct. Returns an error if required variables are missing.

### `internal/couchdb`

Defines the `Client` interface and its concrete implementation `HTTPClient`. All CouchDB interaction goes through this package. Key methods:

| Method | CouchDB endpoint | Purpose |
|--------|-----------------|---------|
| `AllMetaDocs` | `GET /{db}/_all_docs?include_docs=true` | List all non-chunk, non-deleted documents |
| `GetMeta` / `PutMeta` | `GET`/`PUT /{db}/{id}` | Single meta-document CRUD; PutMeta retries once on 409 Conflict |
| `GetChunk` / `PutChunk` | `GET`/`PUT /{db}/{id}` | Single chunk CRUD; PutChunk treats 409 as "already exists" (content-addressed) |
| `BulkGet` | `POST /{db}/_bulk_get` | Fetch many chunks in one request |
| `BulkDocs` | `POST /{db}/_bulk_docs` | Write many chunks in one request; ignores `conflict` errors |
| `Changes` | `GET /{db}/_changes?feed=continuous` | Long-poll change feed; goroutine reconnects with exponential backoff |
| `GetLocal` / `PutLocal` | `GET`/`PUT /{db}/_local/{id}` | Read/write non-replicated local documents (salt, seq) |
| `ServerInfo` | `GET /{db}` | Fetch database info |

`ErrNotFound` is a sentinel error returned on HTTP 404.

### `internal/crypto`

Handles all E2EE operations. When `E2EEPassword` is empty, the service is a transparent pass-through (base64 encode/decode only). When a password is set:

- `ChunkID(content)` — computes the content-addressed chunk `_id`: `h:<sha256>` (plain) or `h:+<sha256>` (encrypted).
- `EncryptContent(plaintext)` — AES-256-GCM using an HKDF-SHA256 derived key; returns a `%=`-prefixed base64 string (V2 format).
- `DecryptContent(ciphertext)` — detects format by prefix (`%=` = V2 HKDF, `%` = V1 PBKDF2) and decrypts accordingly. Reading V1 is supported for backward compatibility; writing always uses V2.
- `SetSalt([]byte)` — injects the HKDF salt fetched from `_local/obsidian_livesync_sync_parameters`.

### `internal/sync`

Orchestrates the three top-level operations. Holds references to the CouchDB client, crypto service, data directory path, and a `SuppressSet`.

- `Pull(ctx)` — see [`docs/flows.md`](flows.md).
- `Push(ctx)` — see [`docs/flows.md`](flows.md).
- `Watch(ctx)` — starts `RemoteWatcher` and `LocalWatcher` as concurrent goroutines and blocks until one returns or the context is cancelled.
- `applyRemoteDoc` — shared helper used by both `Pull` and the remote watcher callback: fetches chunks via `BulkGet`, assembles, decrypts, and writes to disk.
- `pushFile` — shared helper used by both `Push` and the local watcher callback: reads a file, splits, encrypts, uploads chunks via `BulkDocs`, upserts the meta document.

### `internal/watcher`

Contains two watcher types and the `SuppressSet`.

**`RemoteWatcher`** opens the CouchDB continuous `_changes` feed (via `Client.Changes`) and calls a caller-supplied `onEvent` callback for each `ChangeEvent`. Persists the last seen sequence number to `<dataDir>/.obgo_seq` so that watch mode resumes from the correct position after a restart.

**`LocalWatcher`** uses [fsnotify](https://github.com/fsnotify/fsnotify) to watch the vault directory tree recursively. On `Write`/`Create` events it calls an `onChange` callback; on `Remove`/`Rename` events it calls an `onRemove` callback. Hidden files and directories (dot-prefixed) are skipped. Before invoking callbacks, the watcher checks `SuppressSet.IsSuppressed` to drop events caused by the app's own writes.

**`SuppressSet`** is a thread-safe set of recently-written absolute file paths with a 2-second TTL. `Add(path)` records a write; `IsSuppressed(path)` returns true and lazily evicts expired entries. Used to break the remote-write → fsnotify-event → push feedback loop.

### `lib/livesync`

Shared utilities that implement protocol-level details:

- `EncodeDocID(relPath)` — encodes a vault-relative path into a CouchDB document `_id`, prepending `/` when the path starts with `_` to avoid collisions with CouchDB design documents.
- `Split(content, chunkSize)` — splits file content into chunks following the Livesync chunking rules (line-boundary aware for text; fixed-size base64 blocks for binary). When `chunkSize` is 0 the default sizes from the protocol are used.

---

## Key interfaces

### `couchdb.Client`

```go
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
```

Defined in `internal/couchdb`; implemented by `HTTPClient`. Accepted by `sync.New` so that tests can inject a mock.

### `sync.Service`

Not an interface but the central orchestrator. Created via:

```go
svc := sync.New(db couchdb.Client, cr *crypto.Service, dataDir string) *Service
```

Public methods: `Pull(ctx)`, `Push(ctx)`, `Watch(ctx)`.

### `watcher.SuppressSet`

```go
func NewSuppressSet() *SuppressSet
func (s *SuppressSet) Add(path string)
func (s *SuppressSet) IsSuppressed(path string) bool
```

Shared between `sync.Service` (which calls `Add` before writing to disk) and `LocalWatcher` (which calls `IsSuppressed` before pushing). The single instance is created in `sync.New` and passed to `watcher.NewLocalWatcher`.
