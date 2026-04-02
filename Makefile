.PHONY: test test-integration dev build couchdb

test:
	go test ./...

test-integration:
	go test -tags integration -v ./...

dev:
	go run ./cmd/obgo

build:
	go build -o obgo ./cmd/obgo

couchdb:
	docker compose up -d couchdb
