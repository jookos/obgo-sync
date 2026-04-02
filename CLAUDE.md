# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`obgo-live` is a Go CLI that syncs an Obsidian vault with a CouchDB instance using the Obsidian Livesync protocol. It is a lightweight, headless alternative to the Node.js-based Obsidian Livesync — designed for containerized setups (e.g., alongside a QMD/MPC/LLM stack).

## Commands (once scaffolded per ROADMAP.md)

```bash
make test          # run tests
make dev           # start app in dev mode
make build         # compile the app
make couchdb       # start CouchDB via Docker Compose (for dev/testing)

go test ./...                  # run all tests
go test ./internal/... -run TestName  # run a single test
```

## Configuration

The app reads from environment variables (or a `.env` file):

| Variable       | Description |
|----------------|-------------|
| `COUCHDB_URL`  | Full URL including auth and DB name: `https://<user>:<password>@<host>:<port>/<db>` |
| `E2EE_PASSWORD`| Optional end-to-end encryption key |
| `OBGO_DATA`    | Path to the local vault folder on disk |

## Architecture

Planned Go project layout:

```
cmd/           # CLI entry point; loads config, dispatches to services
internal/      # Business logic (CouchDB client, sync, file watching, E2EE)
lib/           # Shared utility/library code
docs/          # Protocol analysis and implementation plan
docker-compose.yml  # CouchDB for dev/test
Makefile
```

### Key flows

- **Pull**: Fetches all documents from CouchDB, decrypts (if E2EE), writes to `OBGO_DATA`. Local files not in CouchDB are pushed up as new.
- **Push**: Reads `OBGO_DATA`, encrypts (if E2EE), upserts all documents to CouchDB.
- **Watch mode (`--watch` / `-w`)**: Two concurrent goroutines run after the initial pull/push:
  1. CouchDB change feed watcher → applies remote changes to disk
  2. Local filesystem watcher → pushes local changes to CouchDB (ignoring changes made by the app itself to avoid loops)

### Protocol

The CouchDB protocol used is the Obsidian Livesync protocol. Before implementing, analyze the reference implementation at `https://github.com/vrtmrz/obsidian-livesync` and document findings in `docs/livesync-protocol.md`. Do **not** commit the reference repo into this repository.

## Implementation Roadmap

See `ROADMAP.md` for the phased implementation plan (protocol analysis → project scaffold → pull/push/watch → tests → docs).

## Implementation way of working

- implement in clear increments
- for each phase, create a new git branch
- commit early
- squash merge branches providing a good concise summary of all the changes
- keep track of your progress so that if interrupted, you will easily know what to continue on
