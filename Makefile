.PHONY: run up down dev build test tidy fmt vet lint pull-models app-up journal open

# One command from a clean clone: config, database, journal, server, browser.
# Everything it does is idempotent, so it is also the normal way to start up.
run: .env journal up wait-db
	@$(MAKE) --no-print-directory open &
	go run ./cmd/server

# Created from the example on first run. The server refuses to start without a
# key rather than failing on the first question, so say so here.
.env:
	@cp .env.example .env
	@echo
	@echo "  Created .env — add a GEMINI_API_KEY before running."
	@echo "  Free key: https://aistudio.google.com/apikey"
	@echo "  Or set LLM_PROVIDER=ollama to stay fully offline."
	@echo
	@exit 1

wait-db:
	@printf 'waiting for postgres'
	@for i in $$(seq 1 30); do \
		if docker compose exec -T postgres pg_isready -U room -d decision_room >/dev/null 2>&1; then \
			echo " ready"; exit 0; \
		fi; \
		printf '.'; sleep 1; \
	done; \
	echo; echo "postgres did not become ready"; exit 1

# Waits for the server to answer, then opens it. Backgrounded by `run`.
open:
	@for i in $$(seq 1 60); do \
		if curl -fsS http://localhost:8080/health >/dev/null 2>&1; then break; fi; \
		sleep 1; \
	done
	@(open http://localhost:8080 2>/dev/null \
		|| xdg-open http://localhost:8080 2>/dev/null \
		|| echo "open http://localhost:8080") >/dev/null 2>&1 || true

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

# Your journal, created from the shipped example on first run. Never overwrites.
journal:
	@test -d journal || (cp -R journal.example journal && \
		echo "Created ./journal — edit journal/about-me.md to make this yours.")
