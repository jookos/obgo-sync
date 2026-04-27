# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`obgo-live` is a Go CLI that syncs an Obsidian vault with a CouchDB instance using the Obsidian Livesync protocol. It is a headless alternative to the Node.js-based Obsidian Livesync, designed for containerised setups.

## Commands

```bash
make test               # run unit tests (no Docker required)
make test-integration   # run integration tests (requires CouchDB via make couchdb)
make build              # compile to ./obgo
make dev                # go run ./cmd/obgo
make couchdb            # start CouchDB via Docker Compose (localhost:5984, admin/password)

go test ./internal/sync/... -run TestPull   # run a single test by name
go test -tags integration ./...             # integration tests only

# CLI sub-commands (all accept an optional vault-relative path argument)
obgo pull [path]        # pull whole vault, a single file, or a folder (path/ suffix)
obgo push [path]        # push whole vault, a single file, or a folder (path/ suffix)
obgo list [path/]       # list remote vault contents, optionally filtered to a folder
```

## Configuration

Environment variables (or `.env` file loaded at startup):

| Variable        | Required | Description |
|-----------------|----------|-------------|
| `COUCHDB_URL`   | yes      | `https://user:password@host:port/dbname` |
| `OBGO_DATA`     | yes      | Path to local Obsidian vault folder |
| `E2EE_PASSWORD` | no       | Encryption passphrase; empty disables E2EE |

## Architecture

```
cmd/obgo/main.go          cobra CLI; wires config → HTTPClient → crypto.Service → sync.Service
internal/config/          Config struct + Load() from env
internal/couchdb/         Client interface + HTTPClient (net/http against CouchDB REST API)
internal/crypto/          E2EE encrypt/decrypt
internal/sync/            pull.go, push.go, service.go, conflict.go — orchestration + Watch
internal/watcher/         RemoteWatcher (_changes feed), LocalWatcher (fsnotify), SuppressSet
lib/livesync/             Pure helpers: EncodeDocID/DecodeDocID, Split/Assemble chunks
docs/                     livesync-protocol.md, implementation-plan.md, architecture.md, flows.md
```

### Key design decisions

**`couchdb.Client` is an interface** — all business logic depends on it, making unit tests straightforward with a `mockClient` (see `internal/sync/*_test.go`).

**Document ID encoding**: `lib/livesync.EncodeDocID` lowercases the path before using it as the CouchDB `_id`, matching Obsidian's normalisation. The `path` field on `MetaDoc` retains the original casing and is used for writing files to disk.

**Chunking**: files are split into chunks (`lib/livesync/chunk.go`). Each chunk is content-addressed: its `_id` is `h:` + base36(xxhash64(`"${data}-${charCount}"`)) for plain, or `h:+` + the same hash for E2EE vaults. Chunks are uploaded via `_bulk_docs`; `409 Conflict` on a chunk means it already exists and is silently ignored.

**E2EE** (`internal/crypto/crypto.Service`): the V2 format (`%=` prefix) uses a two-step key derivation matching the `octagonal-wheels` library used by the Obsidian plugin:
1. `masterKey = PBKDF2-SHA256(passphrase, pbkdf2Salt, 310000, 32 bytes)` — derived once in `SetSalt` and cached
2. `chunkKey = HKDF-SHA256(IKM=masterKey, salt=perChunkSalt, info="", 32 bytes)` — per chunk

The binary layout of each encrypted chunk data field is `[IV(12)][HKDF_salt(32)][AES-256-GCM ciphertext+tag]`. The `pbkdf2Salt` is stored in `_local/obsidian_livesync_sync_parameters` in CouchDB; `Push` generates and persists a new one if absent. V1 (`%` prefix) uses PBKDF2-SHA512 with a static salt — supported for reading only.

**Conflict resolution** (`internal/sync/conflict.go`): `resolveConflicts` picks the conflicting revision with the highest `mtime` as the winner, rewrites it as the authoritative revision if it differs from CouchDB's chosen winner, and tombstones all losing branches. Called on every meta doc during `Pull` and `Watch`.

**Loop prevention in watch mode**: `watcher.SuppressSet` tracks absolute paths the app just wrote to disk. `LocalWatcher` drops fsnotify events for any suppressed path (2 s TTL, lazy eviction). This prevents the pull→write→fsnotify→push→pull cycle.

**Debounced deletes**: `LocalWatcher` does not call `onRemove` immediately on `Remove`/`Rename` events. Instead it starts a per-path 500ms timer. If a `Create` or `Write` for the same path arrives before the timer fires (as happens during atomic-save in Vim/Neovim), the timer is cancelled and the event is treated as an update. This prevents editors that rename→write from accidentally tombstoning files on the remote.

**Watch mode** (`--watch`): after the initial pull/push, `sync.Service.Watch` starts two goroutines — `RemoteWatcher` streams `_changes?feed=continuous` and calls `applyRemoteDoc`; `LocalWatcher` uses fsnotify and calls `pushFile`. The last CouchDB seq is persisted to `.obgo_seq` inside `OBGO_DATA` for restart resume.

### Integration tests

Files tagged `//go:build integration` use a real CouchDB. Each test creates a database with a random suffix and deletes it in `t.Cleanup`. Run with `make test-integration` after `make couchdb`.

## Way of working

- One git branch per phase; squash-merge to main with a concise summary
- Keep commits small and focused
