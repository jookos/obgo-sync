# Implementation Plan

A concrete blueprint for implementing `obgo-live` in Go, based on the protocol analysis in `docs/livesync-protocol.md`.

---

## Table of Contents

1. [Architecture](#architecture)
2. [Package Dependency Graph](#package-dependency-graph)
3. [Key Interfaces](#key-interfaces)
4. [Pull Flow](#pull-flow)
5. [Push Flow](#push-flow)
6. [Watch Mode](#watch-mode)
7. [E2EE Details](#e2ee-details)
8. [Integration Test Strategy](#integration-test-strategy)

---

## Architecture

```
cmd/obgo/main.go  (cobra CLI: pull / push --watch)
  └─► internal/config      Config struct, Load()
  └─► internal/couchdb     Client interface + HTTPClient
  └─► internal/crypto      Service (HKDF-SHA256 + AES-GCM E2EE)
  └─► internal/sync        SyncService (pull / push / watch orchestration)
  └─► internal/watcher     RemoteWatcher + LocalWatcher + SuppressSet
  └─► lib/livesync         EncodeDocID / DecodeDocID, Split / Assemble
```

### Directory layout

```
cmd/
  obgo/
    main.go          # cobra root command; wires config → sync.Service → run
internal/
  config/
    config.go        # Config struct; Load() reads env / .env
  couchdb/
    client.go        # Client interface
    http_client.go   # HTTPClient implementing Client
    types.go         # MetaDoc, ChunkDoc, ChangeEvent, etc.
  crypto/
    service.go       # Service: key derivation + encrypt/decrypt
  sync/
    service.go       # SyncService: Pull, Push, Watch
  watcher/
    remote.go        # RemoteWatcher: _changes feed loop
    local.go         # LocalWatcher: fsnotify loop
    suppress.go      # SuppressSet: loop-prevention set
lib/
  livesync/
    ids.go           # EncodeDocID, DecodeDocID
    chunks.go        # Split, Assemble
docs/
  livesync-protocol.md
  implementation-plan.md
docker-compose.yml
Makefile
go.mod
```

---

## Package Dependency Graph

No circular imports. Import direction flows downward only.

```
lib/livesync         ← no internal deps
internal/config      ← no internal deps
internal/couchdb     ← no internal deps
internal/crypto      ← lib/livesync
internal/watcher     ← internal/couchdb  (ChangeEvent type only)
internal/sync        ← internal/config
                        internal/couchdb
                        internal/crypto
                        internal/watcher
                        lib/livesync
cmd/obgo             ← internal/config, internal/sync
```

---

## Key Interfaces

### `internal/couchdb.Client`

```go
// Client abstracts all CouchDB HTTP operations needed by obgo-live.
type Client interface {
    // AllMetaDocs returns all meta documents (type=plain|newnote|notes).
    // Deleted docs are included; callers must check _deleted.
    AllMetaDocs(ctx context.Context) ([]MetaDoc, error)

    // BulkGet fetches chunk documents by ID in a single _bulk_get call.
    BulkGet(ctx context.Context, ids []string) ([]ChunkDoc, error)

    // PutMeta creates or updates a meta document.
    // Caller must supply the current _rev when updating.
    PutMeta(ctx context.Context, doc MetaDoc) error

    // PutChunk creates or updates a chunk document.
    PutChunk(ctx context.Context, doc ChunkDoc) error

    // BulkDocs writes multiple documents in one _bulk_docs call.
    // Used for chunk pre-seeding and batch upserts.
    BulkDocs(ctx context.Context, docs []any) error

    // Changes returns a page of changes from the _changes feed starting at seq.
    // Returns documents and the new last_seq value.
    Changes(ctx context.Context, since string, limit int) ([]ChangeEvent, string, error)

    // ChangesLive opens a continuous _changes feed and emits events until ctx is done.
    ChangesLive(ctx context.Context, since string, out chan<- ChangeEvent) error

    // GetLocal fetches a _local/ document by its bare ID (without _local/ prefix).
    GetLocal(ctx context.Context, id string) (map[string]any, error)

    // PutLocal creates or updates a _local/ document.
    PutLocal(ctx context.Context, id string, doc map[string]any) error

    // GetDoc fetches any document by its full _id.
    GetDoc(ctx context.Context, id string) (map[string]any, error)
}
```

### `internal/crypto.Service`

```go
// Service handles all E2EE operations for obgo-live.
// It is safe for concurrent use after SetSalt is called.
type Service interface {
    // SetSalt configures the HKDF salt decoded from pbkdf2salt in SyncParameters.
    // Must be called before any encrypt/decrypt operation.
    SetSalt(salt []byte)

    // EncryptContent encrypts plaintext chunk data.
    // Returns a %=-prefixed ciphertext string (V2 HKDF format).
    EncryptContent(plaintext string) (string, error)

    // DecryptContent decrypts a chunk data string.
    // Detects V1 (% prefix) or V2 (%=  prefix) automatically.
    DecryptContent(ciphertext string) (string, error)

    // ChunkID derives the chunk document _id for a given chunk content.
    // Returns "h:+<hash>" when E2EE is enabled, "h:<hash>" otherwise.
    ChunkID(content string) string

    // Enabled reports whether E2EE is active (passphrase was provided).
    Enabled() bool
}
```

### `internal/sync.Service`

```go
// Service orchestrates pull, push, and watch operations.
type Service interface {
    // Pull fetches all remote documents and writes them to OBGO_DATA.
    // Local files not present in CouchDB are pushed as new.
    Pull(ctx context.Context) error

    // Push reads all files from OBGO_DATA and upserts them to CouchDB.
    Push(ctx context.Context) error

    // Watch runs an initial Pull then starts concurrent watchers.
    // Blocks until ctx is cancelled.
    Watch(ctx context.Context) error
}
```

### `internal/watcher.SuppressSet`

```go
// SuppressSet tracks vault-relative file paths that were written by the app.
// It is used by LocalWatcher to drop fsnotify events triggered by the app's
// own writes, preventing push→pull→push feedback loops.
type SuppressSet interface {
    // Add marks path as suppressed for a short TTL (e.g. 2 seconds).
    Add(path string)

    // IsSuppressed reports whether path is currently suppressed.
    IsSuppressed(path string) bool
}
```

---

## Pull Flow

Pull synchronises CouchDB → local disk. After writing all remote files it
also pushes any local files that have no counterpart in CouchDB.

```
1.  GET _local/obsidian_livesync_sync_parameters
    → decode pbkdf2salt (base64 → []byte)
    → crypto.Service.SetSalt(salt)

2.  couchdb.Client.AllMetaDocs()
    → filter: type == "plain" || type == "newnote"
    → skip docs where _deleted == true

3.  For each MetaDoc:
    a. couchdb.Client.BulkGet(metaDoc.Children)
       → fetch all chunk documents in one request
    b. lib/livesync.Assemble(chunks)
       → concatenate chunk .Data fields in Children order
       → base64-decode if type == "newnote"
    c. crypto.Service.DecryptContent(assembled)
       → no-op when E2EE is disabled
    d. watcher.SuppressSet.Add(localPath)
       → suppress fsnotify event for the file about to be written
    e. os.WriteFile(filepath.Join(obgoData, localPath), content, 0644)
       → create parent directories as needed

4.  Walk OBGO_DATA with filepath.WalkDir
    → build set of local paths
    → for each local path not in the CouchDB meta-doc set: call pushFile()
```

---

## Push Flow

Push synchronises local disk → CouchDB. Each file is split into chunks,
optionally encrypted, and upserted as chunk + meta documents.

```
1.  filepath.WalkDir(OBGO_DATA)
    → skip hidden files/dirs (names starting with ".")
    → for each regular file: call pushFile(path)

pushFile(path):
2.  os.ReadFile(path) → rawBytes

3.  lib/livesync.Split(rawBytes, fileType)
    → returns []string (plaintext) or []string (base64 chunks for binary)
    → chunk size: MAX_DOC_SIZE=1000 chars (text) / MAX_DOC_SIZE_BIN=102400 bytes (binary)

4.  For each chunk:
    a. encrypted = crypto.Service.EncryptContent(chunk)   // no-op if disabled
    b. id = crypto.Service.ChunkID(chunk)
    c. collect {_id: id, type: "leaf", data: encrypted, e_: true}

5.  couchdb.Client.BulkDocs(newChunks)
    → only send chunks that don't already exist (check via AllDocs by keys)

6.  Resolve _rev for existing MetaDoc (GET /{db}/{encodedID} → _rev)

7.  couchdb.Client.PutMeta(MetaDoc{
        ID:       lib/livesync.EncodeDocID(vaultRelPath),
        Type:     "plain" | "newnote",
        Path:     vaultRelPath,
        Children: []string{chunk IDs in order},
        CTime:    fileInfo.ModTime().UnixMilli(),  // use mtime for both on first push
        MTime:    fileInfo.ModTime().UnixMilli(),
        Size:     int64(len(rawBytes)),
    })
```

---

## Watch Mode

Watch mode runs after an initial Pull and keeps local disk and CouchDB
continuously in sync using two concurrent goroutines.

```
func (s *syncService) Watch(ctx context.Context) error {
    // 1. Initial pull to reach a consistent starting state.
    if err := s.Pull(ctx); err != nil { return err }

    // 2. Load persisted lastSeq from .obgo_seq (if present).
    lastSeq := loadSeq()

    suppress := watcher.NewSuppressSet()

    var wg sync.WaitGroup
    wg.Add(2)

    // 3. RemoteWatcher goroutine.
    go func() {
        defer wg.Done()
        rw := watcher.NewRemoteWatcher(client, suppress, obgoData)
        rw.Run(ctx, lastSeq)   // updates lastSeq and persists to .obgo_seq
    }()

    // 4. LocalWatcher goroutine.
    go func() {
        defer wg.Done()
        lw := watcher.NewLocalWatcher(obgoData, suppress, syncService)
        lw.Run(ctx)
    }()

    wg.Wait()
    return ctx.Err()
}
```

### RemoteWatcher

- Calls `couchdb.Client.ChangesLive(ctx, lastSeq, eventChan)`.
- For each `ChangeEvent`:
  - If `_deleted`: remove file from disk.
  - If `type == "plain" || "newnote"`: BulkGet → Assemble → Decrypt → SuppressSet.Add → WriteFile.
- After each successful write: persist `lastSeq` to `.obgo_seq` in `OBGO_DATA`.

### LocalWatcher

- Uses `github.com/fsnotify/fsnotify` to watch `OBGO_DATA` recursively.
- On `Write` or `Create` event:
  - If `SuppressSet.IsSuppressed(path)` → skip (was written by RemoteWatcher).
  - Otherwise: call `pushFile(path)` to upload the changed file.
- On `Remove` event:
  - Upsert MetaDoc with `_deleted: true`.

### Loop prevention

```
RemoteWatcher writes file → SuppressSet.Add(path)
                          → LocalWatcher sees event, checks IsSuppressed → drops it
```

The suppress entry expires after a short TTL (2 seconds) to avoid permanently
silencing legitimate rapid user edits.

---

## E2EE Details

### Salt bootstrap

```
GET /{db}/_local/obsidian_livesync_sync_parameters
  → doc.pbkdf2salt (base64-encoded, 16–32 bytes)
  → base64.StdEncoding.DecodeString(doc["pbkdf2salt"])
  → crypto.Service.SetSalt(saltBytes)

If the document does not exist or pbkdf2salt is absent:
  → generate 32 random bytes
  → PUT the document back (create it)
  → use those bytes as salt
```

### Key derivation (V2 — default)

```go
// hkdf.New returns an io.Reader; read 32 bytes for AES-256.
h := hkdf.New(sha256.New, []byte(passphrase), salt, nil)
key := make([]byte, 32)
io.ReadFull(h, key)
```

### Encryption (V2)

```go
block, _ := aes.NewCipher(key)
gcm, _   := cipher.NewGCM(block)

nonce := make([]byte, gcm.NonceSize())
rand.Read(nonce)

ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
// Encode as:  "%=" + base64(ciphertext)
```

### Decryption

```
if strings.HasPrefix(data, "%=") → V2 HKDF: strip prefix, base64-decode, AES-GCM decrypt
if strings.HasPrefix(data, "%")  → V1 PBKDF2: strip prefix, decrypt with PBKDF2-SHA512 key
otherwise                        → unencrypted, use data verbatim
```

### Chunk ID derivation

```
E2EE disabled:  id = "h:"  + xxhash64hex(content)
E2EE enabled:   id = "h:+" + sha256hex(content + passphrase)
```

The `h:+` prefix marks the chunk as belonging to an encrypted vault and allows
unencrypted clients to skip it gracefully.

### Ciphertext prefix summary

| Prefix | Meaning |
|--------|---------|
| `%=`   | V2 HKDF-SHA256 + AES-256-GCM |
| `%`    | V1 PBKDF2-SHA512 + AES-GCM (legacy, read-only) |
| (none) | Unencrypted |

---

## Integration Test Strategy

### Build tag

All integration tests are gated behind a build tag so they never run in unit-test CI:

```go
//go:build integration
```

### Test DB lifecycle

Each test creates a fresh CouchDB database with a random suffix and cleans
up in `t.Cleanup`:

```go
func newTestDB(t *testing.T) couchdb.Client {
    t.Helper()
    dbName := fmt.Sprintf("obgo-test-%s", randomSuffix(8))
    client := couchdb.NewHTTPClient(testCouchDBURL(dbName))
    require.NoError(t, client.CreateDB(context.Background()))
    t.Cleanup(func() {
        _ = client.DeleteDB(context.Background())
    })
    return client
}
```

### Running integration tests

```bash
# Start CouchDB first (docker-compose.yml provides CouchDB 3.x at localhost:5984)
make couchdb

# Run integration tests
go test -tags integration ./...

# Or target a specific package
go test -tags integration ./internal/sync/...
```

### Environment for integration tests

```
COUCHDB_URL=http://admin:password@localhost:5984/<db>
E2EE_PASSWORD=testpassword   # optional; set to test E2EE paths
OBGO_DATA=/tmp/obgo-test-vault
```

### Test scenarios to cover

| Scenario | Package |
|----------|---------|
| Push a file, verify MetaDoc + chunks appear in CouchDB | `internal/sync` |
| Pull a MetaDoc, verify file written to disk | `internal/sync` |
| Push → Pull round-trip (content identity) | `internal/sync` |
| Push → Pull with E2EE enabled | `internal/sync` |
| Watch: remote change → file appears on disk | `internal/watcher` |
| Watch: local file write → appears in CouchDB | `internal/watcher` |
| Watch loop prevention: app write does not re-trigger push | `internal/watcher` |
| Salt bootstrap: missing SyncParameters doc → auto-created | `internal/crypto` |
| Chunk ID: encrypted vs plain | `internal/crypto` |
| EncodeDocID / DecodeDocID round-trip | `lib/livesync` |
| Split → Assemble round-trip for text and binary | `lib/livesync` |
