# Obsidian Livesync Protocol Reference

A developer reference for implementing the Obsidian Livesync CouchDB protocol in Go (or any other language), derived from analysis of the [obsidian-livesync](https://github.com/vrtmrz/obsidian-livesync) plugin and its [livesync-commonlib](https://github.com/vrtmrz/livesync-commonlib) submodule.

---

## Table of Contents

1. [Overview](#overview)
2. [Database Layout](#database-layout)
3. [Document Types](#document-types)
4. [Document ID Encoding](#document-id-encoding)
5. [Chunking / Splitting](#chunking--splitting)
6. [E2EE (End-to-End Encryption)](#e2ee-end-to-end-encryption)
7. [Special Documents](#special-documents)
8. [Change Feed Usage](#change-feed-usage)
9. [Replication Protocol](#replication-protocol)
10. [Conflict Resolution](#conflict-resolution)
11. [Compatibility Check Sequence](#compatibility-check-sequence)

---

## Overview

Obsidian Livesync uses CouchDB (or a compatible server like IBM Cloudant) as a relay. Every vault file is stored as one or more CouchDB documents. Binary and large text files are split into **chunk** documents; a **meta** document references them by ID. The protocol is essentially PouchDB replication between the local PouchDB (in the plugin) and the remote CouchDB.

The plugin does not call CouchDB directly for most operations — it relies on PouchDB's built-in replication which calls `/_changes`, `/_bulk_get`, `/_bulk_docs` etc. under the hood. For chunk pre-seeding it uses `allDocs` and `bulkDocs` directly.

---

## Database Layout

| Concern | Value |
|---------|-------|
| Database name suffix | `<vault-name>-livesync-v2` (from `SuffixDatabaseName`) |
| Protocol/compatibility version | `VER = 12` (as of plugin 0.25.0+) |
| Max plain-text chunk size | 1 000 characters (`MAX_DOC_SIZE`) |
| Max binary chunk size | 102 400 bytes / ~100 KB (`MAX_DOC_SIZE_BIN`) |

---

## Document Types

All documents have a `type` field that identifies their role. The full set of `type` string values:

| `type` string | Go type name (suggested) | Description |
|---------------|--------------------------|-------------|
| `"plain"` | `PlainEntry` | Text/markdown file meta-doc (chunks stored separately) |
| `"newnote"` | `NewEntry` | Binary file meta-doc (chunks stored separately) |
| `"notes"` | `NoteEntry` | **Legacy** — inline data, no chunks; kept for backward compat |
| `"leaf"` | `EntryLeaf` | A single content chunk |
| `"chunkpack"` | `EntryChunkPack` | Packed multiple chunks into one doc (future use) |
| `"versioninfo"` | `EntryVersionInfo` | Database version marker |
| `"syncinfo"` | `SyncInfo` | Canary doc used to verify E2EE decryption works |
| `"sync-parameters"` | `SyncParameters` | Stores PBKDF2 salt and protocol version |
| `"milestoneinfo"` | `EntryMilestoneInfo` | Node registry and compatibility matrix |
| `"nodeinfo"` | `EntryNodeInfo` | Per-device node ID (stored as `_local/`) |

### Meta document (PlainEntry / NewEntry)

```json
{
  "_id": "<encoded-path>",
  "_rev": "...",
  "type": "plain",
  "path": "Notes/hello.md",
  "children": ["h:abc123...", "h:def456..."],
  "ctime": 1700000000000,
  "mtime": 1700000001000,
  "size": 4200,
  "eden": {}
}
```

Field notes:
- `path` — vault-relative file path including any prefix (e.g. `"i:config/..."` for plugin data)
- `children` — ordered list of chunk document `_id`s that reconstruct the file when concatenated
- `ctime` / `mtime` — Unix milliseconds
- `size` — byte size of the original file
- `eden` — small inline data map for tiny recently-modified chunks (optimization, can be treated as opaque)
- `_deleted: true` — soft-delete: file was removed

For `"newnote"` the structure is identical; only `type` differs. The distinction affects how content is decoded (binary vs. plain text).

### Chunk document (EntryLeaf)

```json
{
  "_id": "h:abc123def456...",
  "type": "leaf",
  "data": "<base64-or-plain-text>",
  "e_": true
}
```

Field notes:
- `_id` starts with `h:` (see ID encoding section)
- `data` — raw chunk content. For plain files it is UTF-8 text. For binary files it is base64.
- `e_: true` — present when the chunk is encrypted
- `isCorrupted: boolean` — optional, set when decryption fails

---

## Document ID Encoding

### Source: `src/string_and_binary/path.ts` (`path2id_base`)

The document `_id` is derived from the vault-relative file path with the following rules:

#### Unencrypted (no E2EE / no path obfuscation)

```
path → _id

Rules:
1. If path starts with "_", prepend "/"  →  "/_ ..."
   (avoids CouchDB design doc collision)
2. Use path verbatim as _id
```

Examples:
- `Notes/hello.md` → `Notes/hello.md`
- `_attachments/img.png` → `/_attachments/img.png`

#### With path obfuscation (`obfuscatePassphrase` set)

```
hashedPassphrase = SHA-256^N(SALT_OF_ID + passphrase)
_id = f: + SHA-256(hashedPassphrase + ":" + filename)
```

Where `SALT_OF_ID = "a83hrf7f\x03y7sa8g31"` and `f:` is the `PREFIX_OBFUSCATED` prefix.

#### Chunk IDs

Chunk `_id` values always start with `h:` (the `PREFIX_CHUNK` / `IDPrefixes.Chunk`).

The hash is computed over the chunk content using a MurmurHash variant (xxHash / MurmurHash with seed `0x12345678`), mixed with a passphrase salt when encryption is enabled:

```
plain hash:     h:<murmur-or-xxhash-hex>
encrypted hash: h:+<murmur-or-xxhash-hex>   (note the "+" prefix after "h:")
```

The `+` prefix inside the chunk ID (i.e. `IDPrefixes.EncryptedChunk = "h:+"`) marks the chunk as belonging to an encrypted vault.

#### ID prefixes summary

| Prefix | Meaning |
|--------|---------|
| `h:` | Chunk document |
| `h:+` | Chunk document, encrypted vault |
| `f:` | File meta-doc with obfuscated (hashed) path |
| `i:` | Internal plugin file (config, snippets, etc.) |
| (none) | Regular file, plaintext path |

---

## Chunking / Splitting

Large files are split before being stored as multiple `leaf` documents. The algorithm depends on file type and settings.

### Plain text (`.md`, `.txt`, `.canvas`)

1. Split on line boundaries.
2. Respect code fences (``` ``` ```) — keep blocks together.
3. Target piece size: `MAX_DOC_SIZE = 1000` characters.
4. Minimum chunk size is configurable (default varies by splitter version).
5. A V2 splitter uses `Intl.Segmenter` for sentence-aware splitting when available.

### Binary files

1. Base64-encode the binary content.
2. Split into fixed-size chunks of `MAX_DOC_SIZE_BIN = 102400` characters.

### Reassembly

Concatenate chunk `data` fields in the order given by `children` in the meta doc. For binary files, base64-decode the result.

---

## E2EE (End-to-End Encryption)

Livesync has two encryption generations. Both use the same passphrase but different KDF/cipher approaches.

### V1 (legacy, `E2EEAlgorithms.ForceV1`)

- **KDF**: PBKDF2-SHA-512, iterations = `length(passphrase) * 1000` (dynamic) or fixed 1000 iterations
- **Cipher**: AES-GCM or similar (implemented in the `octagonal-wheels` `encrypt`/`decrypt` functions)
- **Salt**: A static string constant `SALT_OF_PASSPHRASE = "rHGMPtr6oWw7VSa3W3wpa8fT8U"` baked into the library
- **Prefix markers** on encrypted `data` strings: `%` for V1, `%=` for HKDF upgrade within V1 path

### V2 (current default, `E2EEAlgorithms.ADVANCED_E2EE` / HKDF)

- **KDF**: HKDF-SHA-256 with an ephemeral salt stored in the `SyncParameters` document on the server
- **Cipher**: AES-256-GCM (via Web Crypto API)
- **Salt storage**: `_local/obsidian_livesync_sync_parameters` document, field `pbkdf2salt` (base64-encoded, 16-32 bytes)
- **Prefix marker**: `%=` on encrypted strings (distinguishes from V1's `%`)

The V2 HKDF variant with ephemeral salted encryption is also called "ephemeral salt" encryption (`encryptWithEphemeralSalt` / `HKDF_SALTED_ENCRYPTED_PREFIX`).

### What gets encrypted

When E2EE is enabled, the following fields/documents are encrypted:

| Item | How |
|------|-----|
| `EntryLeaf.data` (chunks) | The entire `data` string is replaced with the encrypted ciphertext; `e_: true` is added |
| `SyncInfo.data` (canary) | Same as chunk data |
| `AnyEntry.path` (meta docs) | When path obfuscation is on — path is replaced with `"/\\:" + encrypted(JSON({path,mtime,ctime,size,children}))` |
| `AnyEntry.eden` (inline chunks) | Encrypted as a JSON blob under key `"h:++encrypted"` (V1) or `"h:++encrypted-hkdf"` (V2) |

Non-encrypted fields on meta docs: `_id`, `_rev`, `type`, `children` (IDs remain as hashes, not plaintext).

### Encryption detection

When reading a chunk:
1. If `e_` field is absent or false → unencrypted, use `data` directly
2. If `e_` is true and `data` starts with `%=` → V2 HKDF decryption
3. If `e_` is true and `data` starts with `%` → V1 PBKDF2 decryption
4. Otherwise → unknown, treat as error

### Path obfuscation detection

When reading a meta doc with path obfuscation:
1. If `path` starts with `/\\:` → V2 HKDF-encrypted metadata blob
2. If path looks obfuscated (non-printable, random hex) → V1 encrypted path

---

## Special Documents

### Version document

```
_id:  "obsydian_livesync_version"
type: "versioninfo"
version: 12
```

This is the first thing clients check on connect. If the version is lower than the client's `VER`, the client runs migration and bumps the version. If the version is higher than the client supports, replication is refused.

### SyncInfo canary

```
_id:  "syncinfo"
type: "syncinfo"
data: "<30-char random string, or encrypted ciphertext>"
```

Used to verify that E2EE decryption works end-to-end. On first setup, a 30-character random alphanumeric string is written. With E2EE enabled, `data` is encrypted. If a client cannot decrypt it, E2EE passwords do not match.

### SyncParameters document

```
_id:  "_local/obsidian_livesync_sync_parameters"
type: "sync-parameters"
protocolVersion: 2
pbkdf2salt: "<base64-encoded random bytes>"
```

Stored as a CouchDB `_local` document (not replicated). Each CouchDB server has its own copy. When a client connects:
1. Fetches this doc from the remote (via a direct `GET /{db}/_local/obsidian_livesync_sync_parameters`).
2. If not found, creates it with a freshly generated `pbkdf2salt`.
3. If found but `pbkdf2salt` is empty, adds a salt and writes it back.
4. Salt is decoded from base64 and used as the HKDF salt for all V2 encryption operations.

### Milestone document

```
_id:  "_local/obsydian_livesync_milestone"
type: "milestoneinfo"
created: <epoch ms>
locked: false
cleaned: false
accepted_nodes: ["<nodeId>", ...]
node_chunk_info: {
  "<nodeId>": { "min": 0, "max": 2400, "current": 2 }
}
node_info: {
  "<nodeId>": {
    "device_name": "MacBook",
    "vault_name": "MyVault",
    "app_version": "1.5.x",
    "plugin_version": "0.25.x",
    "progress": "12345",
    "last_connected": 1700000000000
  }
}
tweak_values: {
  "<nodeId>": { ... },
  "__PREFERRED__": { ... }
}
```

Stored as `_local` (not replicated). The `currentVersionRange` hardcoded in the replicator is `{ min: 0, max: 2400, current: 2 }`. The compatibility check ensures the intersection of all nodes' `[min, max]` ranges is non-empty.

The `locked` flag is set during database rebuild operations. When `locked: true` and a node is not in `accepted_nodes`, that node is refused replication.

### NodeInfo document

```
_id:  "_local/obsydian_livesync_nodeinfo"
type: "nodeinfo"
nodeid: "<uuid>"
```

Each client device generates a UUID on first run and stores it locally as `_local`. The `nodeid` is used as the key in the milestone's `node_chunk_info` and `node_info` maps.

---

## Change Feed Usage

The plugin uses PouchDB's replication which internally calls the CouchDB `_changes` endpoint. Key parameters used:

| Parameter | Value / Notes |
|-----------|---------------|
| `style` | `all_docs` (PouchDB default) |
| `include_docs` | `true` during initial pull |
| `since` | last known sequence |
| `live` | `true` in watch/continuous mode, `false` for one-shot |
| `retry` | `true` in watch mode |
| `heartbeat` | `30000` ms in watch mode (unless `useTimeouts` setting is true) |
| `batch_size` | configurable, defaults around 50; halved on 413 errors |
| `batches_limit` | configurable; halved on 413 errors |

#### On-demand chunk pull (optional mode)

When `readChunksOnline` is enabled, the pull replication uses a Mango selector to exclude chunks from the initial sync:

```json
{ "selector": { "type": { "$ne": "leaf" } } }
```

Chunks are then fetched individually on demand as files are opened.

---

## Replication Protocol

### One-shot sync (pull/push/both)

Sequence for a typical bidirectional sync:

```
1. Fetch _local/obsidian_livesync_sync_parameters (PBKDF2 salt)
2. GET obsydian_livesync_version  → check VER == 12
3. GET _local/obsydian_livesync_milestone → ensureRemoteIsCompatible()
4. PUT _local/obsydian_livesync_milestone (update node_info, last_connected, tweak_values)
5. Pre-send chunks (optional: local.changes since lastSeq, type=leaf → bulkDocs to remote)
6. PouchDB.sync(local, remote, { batch_size, batches_limit })
   → internally: /_changes, /_bulk_get, /_bulk_docs
7. Handle conflicts (see below)
```

### Watch / continuous sync

```
1. Run one-shot pullOnly sync to get current state
2. Start live bidirectional PouchDB.sync with live:true, retry:true, heartbeat:30000
3. On "change" event (direction=pull): process incoming docs
4. On "change" event (direction=push): count sent docs
5. On error: terminate and potentially retry with smaller batch sizes
```

### Chunk pre-seeding (sendChunks)

Before normal replication, the client proactively pushes chunks that the remote does not have:

```
1. localDB.changes({ since: lastSeq, selector: {type: "leaf"} })
2. For each chunk batch: remoteDB.allDocs({ keys: [...ids] })
3. Filter to only chunks missing on remote
4. localDB.allDocs({ keys: missing, include_docs: true })
5. Apply E2EE encryption to each chunk (preprocessOutgoing)
6. remoteDB.bulkDocs(chunks, { new_edits: false })
7. Track max seq transferred in _local/max_seq_on_chunk-<remoteID>
```

Maximum batch size: 200 documents or `sendChunksBulkMaxSize` MB (configurable), whichever is smaller.

### Incoming document processing

When a document arrives via replication:
1. Decrypt if `e_` is set.
2. Decrypt path if `path` starts with `/\\:`.
3. Decrypt `eden` if it contains `h:++encrypted` or `h:++encrypted-hkdf`.
4. If `type` is `"plain"` or `"newnote"`: fetch all chunks listed in `children`, assemble, write to disk.
5. If `type` is `"notes"`: use inline `data` field directly (legacy).
6. If `_deleted: true` or `deleted: true`: remove from disk.
7. Check for conflicts (see below).

---

## Conflict Resolution

CouchDB allows multiple revisions to coexist as conflicts. Livesync's strategy:

### Auto-merge (text files)

Implemented in `ConflictManager`:

1. Retrieve all conflicted revisions via `GET /{db}/{id}?conflicts=true`.
2. For each conflicted pair, load both versions plus the common ancestor (oldest shared rev).
3. Run `diff-match-patch` line-by-line diff of ancestor→left and ancestor→right.
4. Apply merge rules:
   - Both sides deleted same line → keep deletion
   - Same insertion on both sides → keep once
   - Both sides insert different content at same position → include both (ordered by `mtime`, older first)
   - One side inserts, other leaves unchanged → include insertion
   - True conflict (same line changed differently on both sides) → abort auto-merge
5. If auto-merge succeeds: write merged content as a new revision, delete the losing revision.
6. If auto-merge fails: present both versions to the user for manual resolution.

### Conflict for binary files

Binary files cannot be merged. The conflict is surfaced to the user to choose a winner.

### Deletion vs. modification conflicts

If one side deleted the file and the other modified it, the modification wins (soft-delete is overwritten by a newer content revision).

---

## Compatibility Check Sequence

Called on every connection to the remote (`ensureRemoteIsCompatible`):

```
1. Fetch _local/obsydian_livesync_milestone from remote
2. If not found: create a new milestone with this device as the only node
3. Check chunk version ranges:
   globalMin = max(all nodes' min)
   globalMax = min(all nodes' max)
   if globalMax < globalMin → INCOMPATIBLE (unless ignoreVersionCheck setting)
4. Check TweakValues (configuration parameters that must match):
   If current settings differ from preferred settings → MISMATCHED
5. Check locked flag:
   locked=true and nodeId not in accepted_nodes → NODE_LOCKED or NODE_CLEANED
   locked=true and nodeId in accepted_nodes → LOCKED (warn, allow read)
6. Update milestone with current device's:
   - chunk version range
   - tweak_values
   - node_info (device_name, vault_name, app_version, plugin_version, progress, last_connected)
7. Return OK if all checks pass
```

The `last_connected` timestamp is only updated if more than 60 seconds have elapsed since the last update, to reduce write churn.

---

## Implementation Notes for Go

### HTTP vs. PouchDB replication

A Go implementation without PouchDB needs to call the CouchDB HTTP API directly. The key endpoints are:

| Operation | Endpoint |
|-----------|----------|
| Get a document | `GET /{db}/{id}` |
| Put/update a document | `PUT /{db}/{id}` |
| Bulk write | `POST /{db}/_bulk_docs` |
| Bulk read | `POST /{db}/_bulk_get` |
| All docs (by keys) | `POST /{db}/_all_docs?include_docs=true` with `{"keys": [...]}` |
| Changes feed (one-shot) | `GET /{db}/_changes?since=<seq>&include_docs=true&limit=<n>` |
| Changes feed (continuous) | `GET /{db}/_changes?feed=continuous&since=<seq>&heartbeat=30000` |
| Find (Mango) | `POST /{db}/_find` |
| Purge | `POST /{db}/_purge` |

### Authentication

Pass credentials as HTTP Basic Auth. The `COUCHDB_URL` env var format is:
```
https://<user>:<password>@<host>:<port>/<dbname>
```

### Local document access

`_local/` documents are not replicated and can be read/written with normal `GET`/`PUT` requests to `/{db}/_local/{id}`.

### Sequence tracking

For watch mode, persist the last received `last_seq` from `_changes` to resume after restart. A reasonable storage location is a local file or a `_local/` document.

### E2EE key derivation (V2)

```
1. Fetch pbkdf2salt from _local/obsidian_livesync_sync_parameters (base64 → []byte)
2. key = HKDF-SHA256(passphrase, salt, info="")
3. Decrypt each chunk's data field with AES-256-GCM using key
   - The ciphertext format embeds its own ephemeral nonce
   - Prefix "%=" identifies HKDF-encrypted data
```

For V1 fallback:
```
key = PBKDF2-SHA512(passphrase, SALT_OF_PASSPHRASE, iterations, 32)
iterations = len(passphrase) * 1000   (dynamic mode)
           = 1000                      (fixed mode)
Prefix "%" identifies V1-encrypted data
```

When implementing, support reading V1 for backward compatibility but always write V2.
