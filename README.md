# Decision Room

A private thinking partner that remembers. Write markdown into a folder; ask it
questions about your own decisions, plans and days. Everything runs on your
machine — one Go binary and a Postgres container.

It is built around a simple idea: an assistant that has read everything you have
written about your own work gives better advice than one that has not.

> **Status:** early. The retrieval engine, chat and journal watcher work today.
> Conversation modes, the generals roster, depth tiers and proactive check-ins
> are landing incrementally — see [the roadmap](#roadmap).

## Quick start

You need Docker and Go 1.24+.

```bash
git clone https://github.com/an4eetos/decision-room && cd decision-room
cp .env.example .env          # then put a Gemini API key in it
make journal                  # creates ./journal from the shipped example
make up                       # starts Postgres
make dev                      # migrates and serves on :8080
```

Get a free Gemini key at [aistudio.google.com/apikey](https://aistudio.google.com/apikey).
Prefer to run entirely offline? Set `LLM_PROVIDER=ollama`, then `make up-ollama`
and `make pull-models`.

Open <http://localhost:8080>.

## How memory works

Two layers, deliberately.

**Always in context** — `journal/about-me.md` and everything under
`journal/context/`. Who you are, how you work, what you have already ruled out.
No size cap. These are facts the assistant should never have to look up.

**Retrieved on demand** — everything else in `journal/`, plus anything you paste
into `/ingest`. Chunked by markdown heading, embedded, and searched per question
with hybrid retrieval: pgvector cosine similarity and Postgres full-text search,
merged with reciprocal rank fusion, then reranked for recency and diversity.

```
journal/
  about-me.md     always injected
  context/        always injected — any number of .md files
  daily/          retrieved
  anything.md     retrieved
```

The folder is watched. Save a file and it is ingested within a second.

## Configuration

Copy `.env.example` to `.env`. Real environment variables take precedence.

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | `postgres://room:room@localhost:5432/decision_room?sslmode=disable` | |
| `LLM_PROVIDER` | `gemini` | `gemini` or `ollama` |
| `GEMINI_API_KEY` | — | required when provider is `gemini` |
| `GEMINI_CHAT_MODEL` | `gemini-2.0-flash` | |
| `GEMINI_CHAT_MODELS` | — | optional failover order, tried left to right |
| `OLLAMA_CHAT_MODEL` | `qwen3:8b` | use `qwen3:4b` if RAM is tight |
| `JOURNAL_WATCH_DIR` | `./journal` | |
| `RETRIEVAL_TOP_K` | `8` | memories passed to the model |
| `RETRIEVAL_CANDIDATES` | `30` | hybrid pool before reranking |
| `WEB_ROOT` | *(empty)* | empty serves the frontend from inside the binary; set it to `./internal/web/assets` to edit templates and CSS without rebuilding |
| `HTTP_ADDR` | `:8080` | |

Migrations run automatically at startup — there is no separate migrate step and
no need to install goose.

## Architecture

```
cmd/server            fx bootstrap
internal/app          config, pool, HTTP server — everything not domain-specific
internal/memory       domain, ports, usecases, Postgres adapters
internal/journal      filesystem watcher and sync
internal/infra        config, postgres, gemini, ollama, http
internal/web          JSON API, browser UI, embedded templates and static files
migrations            embedded SQL schema
```

Hexagonal: usecases depend on ports, adapters implement them, and `fx` wires it
together. Each module contributes its routes to a value group, so the HTTP
server never imports a domain package.

## API

| Method | Path | |
|---|---|---|
| `GET` | `/health` | |
| `POST` | `/api/memories` | ingest (JSON or multipart upload) |
| `GET` | `/api/memories/search?q=&kind=&tags=` | hybrid search |
| `POST` | `/api/consult` | one-shot question |
| `GET` `POST` | `/api/chat/sessions` | |
| `GET` `DELETE` | `/api/chat/sessions/{id}` | |
| `POST` | `/api/chat/sessions/{id}/messages` | |

```bash
curl -X POST localhost:8080/api/consult \
  -H 'Content-Type: application/json' \
  -d '{"question": "What did I decide about the database?"}'
```

## Roadmap

- [x] Hybrid retrieval, chat with history and summarisation, journal watcher
- [ ] Retrieval fixes: relevance/recency rebalance, full-text query rewriting
- [ ] **Depth tiers** — quick, standard and deep answers
- [ ] **Generals** — pick up to three strategic lenses; they argue, then synthesise
- [ ] **Modes** — plan a day, make a hard call, unstick a stalled task, debrief
- [ ] **Commitments** — open loops tracked from your own conversations
- [ ] **Check-ins** — it asks how the day is going, instead of waiting

## Privacy

With `LLM_PROVIDER=gemini`, your journal text is sent to Google when you ask a
question or ingest a file. With `ollama`, nothing leaves your machine. Postgres
runs locally either way. There is no telemetry and no account.

## Licence

MIT
