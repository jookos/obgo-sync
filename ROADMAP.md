# Implementation roadmap

## 1. pull the obsidian livesync project from github repo and analyze

The github repo is at git clone https://github.com/vrtmrz/obsidian-livesync

Do not commit this into git.

Analyze the livesync protocol, create a docs/livesync-protocol.md file that explains the couchdb protocol concisely.

## 2. Plan the implementation

Make a plan for the go application implementation. Store this at docs/implementation-plan.md.

The features to be supported are the usecases documented in README.md

## 3. Create the golang project structure

The source should be organized according to good golang practices. There should be a internal / lib folder with the logic, a cmd folder for the app front.

Implement
- the configuration structs
- the main executable / entry point that loads the config from an .env or environent, using a good library (if suitable)
- calls on the main business logic services for pushing or pulling
- calls on the main business logic services for watching the files and keeping the local folder in sync with couchdb

The business logic services would stubs at this point. The watching and syncing from couchdb should be two different spawned go functions.

Create a docker compose for running couchdb (to be used for tests and development)

Create initial tests for the implemented feature

Create a Makefile with targets for
- running tests
- starting the app in dev mode
- compiling the app
- starting the couchdb docker compose

## 4. Implement the first business logic

Implement:
- connecting to the couchdb
- fetching the current state of the vault from couchdb and saving it on disk (so a replicate from couchdb to local disk)
- this includes implementing the e2e encryption

Create tests for the features

Create integration tests that leverage the couchdb in the docker compose

## 5. Implement storing to couchdb

Implement:
- Storing a local file to the couchdb, overwriting any current version
- this includes applying the e2e encryption (if defined)

Create integration tests

## 6. Implement monitoring changes in couchdb

Implement:
- watch for changes in couchdb
- if any, sync them to the local file system

## 7. Implement monitoring local files for changes

Implement:
- monitor the files in the local vault. If anything changes, push the new version (or changes) to couchdb
- this should take into account / ignore changes made by the app itself when syncing changes from couchdb

## 8 document

Create documentation for the implementation that present the architecture, and the logic of the most important flows.
 
