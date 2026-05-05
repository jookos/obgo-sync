# obgo

**GitHub**: https://github.com/jookos/obgo-sync

> **Disclaimer:** Most of this project was AI-generated (from ROADMAP.md), with some crucial manual edits. It works for me — both with E2EE encryption and without. Use at your own peril.

`obgo` is a headless Go CLI that syncs an Obsidian vault with a CouchDB instance using the [Obsidian Livesync](https://github.com/vrtmrz/obsidian-livesync) protocol. It is a lightweight alternative to the Node.js-based Obsidian Livesync plugin, designed for containerized or server-side setups where no GUI is available — for example, to keep an Obsidian vault on disk alongside a QMD/MPC/LLM stack so that language-model tooling can read and write vault files.

---

## Installation

**[Human speaking]**

Clone the repo. `make build`, edit an .env file and try `obgo pull -w -v`. Play around with it, if happy, create an image of it (see make image / Dockerfile for inspiration), add it to your stack, mounting the shared data volume across your apps.

---

## Configuration

The app is configured via environment variables (or a `.env` file in the working directory).

| Variable        | Required | Description |
|-----------------|----------|-------------|
| `COUCHDB_URL`   | yes      | Full CouchDB URL including credentials and database name: `https://<user>:<password>@<host>:<port>/<dbname>` |
| `E2EE_PASSWORD` | no       | End-to-end encryption passphrase. Must match the passphrase configured in the Obsidian Livesync plugin if E2EE is enabled. |
| `OBGO_DATA`     | yes      | Absolute path to the local vault directory on disk. |
| `OBGO_PATH_OBFUSCATION` | no | Set to `on`, `off`, or `auto` (default). When enabled, file paths and metadata are encrypted in CouchDB. Only applicable when `E2EE_PASSWORD` is set. |

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

# Pull a single file
obgo pull notes/hello.md

# Pull a folder and all its contents
obgo pull notes/

# Push local vault files to CouchDB
obgo push

# Push a single file
obgo push notes/hello.md

# Push a folder and all its contents
obgo push notes/

# List the entire remote vault
obgo list

# List a folder within the remote vault
obgo list notes/

# Bidirectional watch mode: pull first, then keep vault in sync continuously
obgo pull --watch

# Push then keep watching for local changes and remote changes
obgo push --watch
```

### Command semantics

**`pull [path]`** treats CouchDB as the source of truth. Without a path every remote document is written to the local vault. Provide a vault-relative file path to pull only that file, or a path ending with `/` to pull an entire folder.

**`push [path]`** treats the local vault as the source of truth. Without a path all local files are upserted to CouchDB. Provide a file path or a folder path ending with `/` to restrict the operation to that subset.

**`list [path/]`** prints the contents of the remote vault — one file per line with its size and last-modified time. Pass a folder path ending with `/` to filter to that folder.

**`--watch` / `-w`** (available on `pull` and `push`, ignored when a path argument is given) keeps the process running after the initial sync. Two concurrent goroutines maintain bidirectional sync:
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

`obgo` is compatible with Obsidian Livesync's end-to-end encryption. Set `E2EE_PASSWORD` to the same passphrase as the plugin. The app supports both the current V2 format (PBKDF2-SHA256 → HKDF-SHA256 + AES-256-GCM, `%=` prefix) and the legacy V1 format (PBKDF2-SHA512 + AES-256-GCM, `%` prefix) for reading. New chunks are always written in V2 format.

The app also supports **Path and Property Obfuscation**. When enabled, vault-relative paths and metadata (like chunk lists and timestamps) are encrypted into a single blob and stored under hashed document IDs. This feature is controlled by `OBGO_PATH_OBFUSCATION`. By default (`auto`), `obgo` detects if the remote vault is obfuscated by looking for hashed IDs.

The PBKDF2 salt is read from (and, on push, written to) the `_local/obsidian_livesync_sync_parameters` document in CouchDB — the same document used by the plugin.

---

## Further reading

- [`docs/livesync-protocol.md`](docs/livesync-protocol.md) — detailed reference for the CouchDB document schema, chunking algorithm, E2EE format, and change feed usage.
- [`docs/architecture.md`](docs/architecture.md) — package dependency diagram and responsibility summary.
- [`docs/flows.md`](docs/flows.md) — step-by-step descriptions of the pull, push, and watch flows.
