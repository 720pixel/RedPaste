# RedPaste: build and development tasks.
# The frontend (Node/Vite) is a BUILD-TIME step only; production ships a single
# self-contained Go binary with the compiled assets embedded.

export GOTOOLCHAIN ?= go1.25.11

.PHONY: help dev build web server test test-race vet run run-postgres clean tidy docker-build docker-run

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

web: ## Build the frontend into web/dist (embedded into the binary)
	cd web && npm install && npm run build

server: ## Build the Go server binary (embeds web/dist)
	go build -o redpaste-server ./cmd/redpaste-server

build: web server ## Full production build (frontend + server)

dev: ## Run the server locally with embedded SQLite (no Docker, no Postgres)
	REDPASTE_ENV=development REDPASTE_SQLITE_PATH=redpaste.db go run ./cmd/redpaste-server

run: server ## Build then run the server binary
	./redpaste-server

run-postgres: ## Run against a local PostgreSQL (set REDPASTE_DATABASE_URL)
	REDPASTE_ENV=development go run ./cmd/redpaste-server

test: ## Run Go tests
	go test ./...

test-race: ## Run Go tests with the race detector
	go test -race ./...

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy go.mod
	go mod tidy

docker-build: ## Build the Docker image locally (tag: redpaste)
	docker build -t redpaste .

docker-run: docker-build ## Build and run the image on :8080 with a persistent volume
	docker run -d -p 8080:8080 -v redpaste:/data --name redpaste redpaste

clean: ## Remove build artifacts and local DB
	rm -f redpaste-server redpaste-admin *.db *.db-shm *.db-wal
	rm -rf web/dist/assets
