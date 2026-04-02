# obgo-live - the go obsidian livesync app

This application is a golang implementation of a cli for the obsidian loca live sync (using couchdb).

It was developed as a light-weight alternative to the node-based obsidian livesync setups for keeping an obisidian vault synced on disk on a headless / containerized setup. The reference setup would be this app, to keep the obsidian data on disk in sync both ways, a QMD setup on top of it with an MPC server connected to your preferred LLM setup.

It might get expanded with additional obsidian features in the future.

## Use

The app needs to know:
- the couchdb url, including authentication and db name (https://<user>:<password>@<host>:<port>/<database name>)
- possible e2ee key
- the folder where to keep the vault

These can be provided through environment variables
- COUCHDB_URL
- E2EE_PASSWORD
- OBGO_DATA

Commands:
```
$ obgo-live pull --watch   # pulls the vault from the couchdb and keeps it in sync
$ obgo-live pull           # pulls and quits
$ obgo-live push --watch   # pushes the vault to couchdb, and keeps it in sync
$ obgo-live push           # pushes the vault to couchdb and quits
```

The **pull** command assumes the couchdb has the up-to-date data for existing files. Any local files will be overwritten with the data from couchdb. And local files that do not exist in couchdb will be treated as new files, and synched to couchdb.

The **push** command assumes the local data to be fresher than the couchdb's data. Any files existing in couchdb will be overwritten, and new ones created.

The --watch (or -w) flag will keep the app running, monitoring the data directory and pushing any changes immediately. It will also monitor the couchdb and pull and apply any changes from there to the files immediately.


