.PHONY: up down dev build test tidy fmt vet lint pull-models app-up journal

# Postgres only. That is all you need when LLM_PROVIDER=gemini.
up:
	docker compose up -d postgres

# Add a local Ollama container.
up-ollama:
	docker compose --profile ollama up -d postgres ollama

down:
	docker compose down

# Run the server on the host. Migrations run automatically at startup.
dev:
	go run ./cmd/server

# Run everything in Docker, including the app.
app-up:
	docker compose --profile app up -d --build

build:
	go build -o bin/server ./cmd/server

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

tidy:
	go mod tidy

pull-models:
	docker compose exec ollama ollama pull qwen3:8b
	docker compose exec ollama ollama pull nomic-embed-text

# Create your journal from the shipped example.
journal:
	@test ! -d journal || (echo "journal/ already exists — not overwriting" && exit 1)
	cp -R journal.example journal
	@echo "Created ./journal — edit journal/about-me.md to start."
