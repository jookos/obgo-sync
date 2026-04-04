# obgo-live

**GitHub**: https://github.com/jookos/obgo-sync

`obgo-live` is a headless Go CLI that syncs an Obsidian vault with a CouchDB instance using the [Obsidian Livesync](https://github.com/vrtmrz/obsidian-livesync) protocol. It is a lightweight alternative to the Node.js-based Obsidian Livesync plugin, designed for containerized or server-side setups where no GUI is available — for example, to keep an Obsidian vault on disk alongside a QMD/MPC/LLM stack so that language-model tooling can read and write vault files.

---

## Installation

```bash
# Install from source via Go toolchain
go install github.com/jookos/obgo-sync/cmd/obgo@latest

# Or build locally
make build        # produces ./obgo binary
```

---

## Configuration

The app is configured via environment variables (or a `.env` file in the working directory).

| Variable        | Required | Description |
|-----------------|----------|-------------|
| `COUCHDB_URL`   | yes      | Full CouchDB URL including credentials and database name: `https://<user>:<password>@<host>:<port>/<dbname>` |
| `E2EE_PASSWORD` | no       | End-to-end encryption passphrase. Must match the passphrase configured in the Obsidian Livesync plugin if E2EE is enabled. |
| `OBGO_DATA`     | yes      | Absolute path to the local vault directory on disk. |

A minimal `.env` file:

```dotenv
COUCHDB_URL=http://admin:password@localhost:5984/myvault-livesync-v2
OBGO_DATA=/home/user/vault
```

Use `--env-file <path>` to load a different file (default: `.env` in the current directory).

---

## Usage

```bash
# Pull all documents from CouchDB to the local vault
obgo pull

# Push local vault files to CouchDB
obgo push

# Bidirectional watch mode: pull first, then keep vault in sync continuously
obgo pull --watch

# Push then keep watching for local changes and remote changes
obgo push --watch
```

### Command semantics

**`pull`** treats CouchDB as the source of truth. Existing local files are overwritten with CouchDB data. Local files that do not exist in CouchDB are pushed up as new documents.

**`push`** treats the local vault as the source of truth. All local files are upserted to CouchDB, overwriting any existing CouchDB versions.

**`--watch` / `-w`** (available on both commands) keeps the process running after the initial pull/push. Two concurrent goroutines maintain bidirectional sync:
- A **CouchDB watcher** monitors the `_changes` feed and applies remote changes to disk.
- A **filesystem watcher** monitors `OBGO_DATA` and pushes local changes to CouchDB.

A `SuppressSet` prevents feedback loops: files written to disk by the app are suppressed from being immediately re-pushed.

The last-seen CouchDB change sequence is persisted in `<OBGO_DATA>/.obgo_seq` so that watch mode resumes from where it left off after a restart.

---

## Docker / Development CouchDB

A `docker-compose.yml` is included to spin up a local CouchDB for development and testing:

```bash
# Start CouchDB on localhost:5984 (admin/password)
make couchdb
# or directly:
docker compose up -d couchdb
```

The CouchDB admin UI is available at `http://localhost:5984/_utils`.

You will need to create the target database manually before first use, or let the Obsidian Livesync plugin create it for you.

---

## E2EE compatibility

`obgo-live` is compatible with Obsidian Livesync's end-to-end encryption. Set `E2EE_PASSWORD` to the same passphrase as the plugin. The app supports both the current V2 format (PBKDF2-SHA256 → HKDF-SHA256 + AES-256-GCM, `%=` prefix) and the legacy V1 format (PBKDF2-SHA512 + AES-256-GCM, `%` prefix) for reading. New chunks are always written in V2 format.

The HKDF salt is read from (and, on push, written to) the `_local/obsidian_livesync_sync_parameters` document in CouchDB — the same document used by the plugin.

---

## Further reading

- [`docs/livesync-protocol.md`](docs/livesync-protocol.md) — detailed reference for the CouchDB document schema, chunking algorithm, E2EE format, and change feed usage.
- [`docs/architecture.md`](docs/architecture.md) — package dependency diagram and responsibility summary.
- [`docs/flows.md`](docs/flows.md) — step-by-step descriptions of the pull, push, and watch flows.
