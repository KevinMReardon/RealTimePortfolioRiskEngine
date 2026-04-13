SHELL := /bin/sh

MIGRATIONS_DIR := migrations
DATABASE_URL ?= $(DATABASE_URL)

.PHONY: run test test-integration build migrate-up migrate-down db-reset

run:
	go run ./cmd/server

test:
	go test ./...

# Phase 7 ordering / dedupe / restart / concurrency (requires migrated DB).
test-integration:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required" && exit 1)
	INTEGRATION_DATABASE_URL="$(DATABASE_URL)" go test ./internal/events ./internal/api -count=1

build:
	go build ./cmd/server

migrate-up:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required" && exit 1)
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-down:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required" && exit 1)
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

db-reset:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required" && exit 1)
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up
