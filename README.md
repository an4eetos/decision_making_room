# Decision Room

A private thinking partner that remembers. Write markdown into a folder; ask it
questions about your own decisions, plans and days. Everything runs on your
machine — one Go binary and a Postgres container.

It is built around a simple idea: an assistant that has read everything you have
written about your own work gives better advice than one that has not.

> **Status:** early but usable. Retrieval, chat with depth tiers, the generals
> roster, conversation modes, open loops, check-ins, the journal watcher and the
> relocation planner all work today. See [the roadmap](#roadmap).

## Quick start

You need Docker and Go 1.24+.

```bash
git clone https://github.com/an4eetos/decision-room && cd decision-room
make run
```

The first run creates `.env` and stops, because it needs an API key. Put a free
one from [aistudio.google.com/apikey](https://aistudio.google.com/apikey) in it
and run `make run` again. That starts Postgres, applies migrations, creates your
journal from the shipped example, serves on :8080 and opens your browser.

`make run` is also the everyday command — everything it does is idempotent.

Prefer to stay fully offline? Set `LLM_PROVIDER=ollama` in `.env`, then
`make up-ollama && make pull-models` before `make run`. Nothing leaves your
machine in that mode.

## How memory works

Two layers, deliberately.

**Always in context** — `journal/about-me.md` and everything under
`journal/context/`. Who you are, how you work, what you have already ruled out.
No size cap. These are facts the assistant should never have to look up.

**Retrieved on demand** — everything else in `journal/`, plus anything you paste
into `/ingest`. Chunked by markdown heading, embedded, and searched per question
with hybrid retrieval: pgvector cosine similarity and Postgres full-text search,
merged with reciprocal rank fusion, then reranked for recency and diversity.

Chunks carry their heading path, so a chunk that says *"transactions mattered
more than scale"* also carries *"Database choice > Why"* — into the embedding and
the search index, not just the display. Oversized sections overlap, so a point
made across a boundary is findable from both sides.

Every vector records which model produced it. Switching embedding provider
otherwise produces dimensionally valid, semantically meaningless comparisons
that Postgres will never flag — instead, mismatched rows are excluded from
search and the server says so loudly at startup. `POST /api/admin/reindex`
re-embeds them, in resumable batches.

```
journal/
  about-me.md     always injected
  context/        always injected — any number of .md files
  daily/          retrieved
  anything.md     retrieved
```

The folder is watched. Save a file and it is ingested within a second.

## Answer depth

Every question runs at one of three depths, picked under the composer.

| Depth | Retrieval | Digging | For |
|---|---|---|---|
| **Quick** | top 3, no tools | none | "what's next" — seconds, one recommendation |
| **Standard** | top 8, full hybrid search | one round if context is thin | the default |
| **Deep** | question split into facets, pool of 60, model-reranked | up to three rounds | a decision worth the wait |

A session remembers the depth you last used, so a conversation you took deep
stays deep. If Quick retrieves nothing at all it escalates itself to Standard
rather than answering from no context, and every answer is labelled with the
depth that actually produced it.

Deep adds three passes the cheaper tiers cannot justify: it breaks your question
into separate searches and fuses the results, has the model re-rank what came
back, and can pull a general's full doctrine when the summary is not enough.
Each costs a model call, and **each degrades silently** — a failed decomposition
searches the question as asked, a failed re-rank keeps the heuristic order.
None of them can make an answer worse than not running them.

Set `DEFAULT_TIER` and `MAX_TIER` to bound this per deployment. `MAX_TIER=quick`
turns off tool calling entirely.

## Modes

The room works out what kind of question you asked and answers in that shape.
*"I've been avoiding this for a week"* becomes a **Stalled** answer that diagnoses
which of five kinds of stuck this is before prescribing anything. *"Everything is
urgent"* becomes **Overloaded**: a forced ranking, deferrals with dates, and a
kill list.

Fifteen modes across four families — plan, decide, unblock, review — plus **Open**,
which imposes nothing, because not every question should be forced into a
framework.

The detected mode shows in a chip above the conversation along with why it was
picked, and you can pin one. A conversation stays in its mode unless you clearly
change the subject: switching the shape of the answer underneath you mid-thread
is worse than being in a slightly wrong mode.

Modes also bias retrieval. A debrief weights recency heavily; a pre-mortem almost
ignores it, because a decision from a year ago is still the answer to a question
about that decision.

Write your own by dropping a markdown file in `MODES_DIR`. See
[docs/modes.md](docs/modes.md).

## Open loops and check-ins

Say *"I'll call the landlord tomorrow and finish the pricing by Wednesday"* in any
conversation and both appear in the sidebar as **proposals** — resolved to real
dates, waiting for you to keep or drop them. Nothing joins the list without your
agreement: a list you never agreed to stops being trusted, then stops being read.
Only what *you* wrote is extracted; the assistant's own suggestions are not your
promises.

Anything open and untouched for two weeks goes stale and leaves the check-ins.
Reminding you about it after that is nagging.

Turn on check-ins and the room starts the conversation:

- **Morning** helps decide what today is for, **midday** checks you are on what
  you meant to be on, **evening** asks what closed. Each names something specific
  — an overdue commitment, a note from yesterday — and ends with one question.
- **Quiet for a while** fires when you have written nothing for six hours during
  the day, and names what was due.
- **Gone quiet** fires once when something goes stale, asking whether you still
  mean it.

Replying to a check-in is just replying; it opens as a normal conversation. A
check-in you never answer never clutters the chat list. There is also a *Check
in now* button, which works whether or not scheduled ones are on.

Slots are checked every minute against the database, so a laptop that slept
through 08:00 gets its morning check-in on wake — and one that slept through the
whole morning does not get it at 23:00. Everything shows up in the app; there is
no push service. Set `CHECKIN_NOTIFY_CMD` for a desktop notification.

Scheduled check-ins cost one quick model call each. Nudges cost nothing — they
are written from what the database already knows.

## Generals

Twenty strategic lenses. Pick up to three under the composer and they argue about
your question; pick none and the room chooses based on what you asked.

The point is the disagreement. Every general declares a blind spot — what it
systematically gets wrong — and a multi-lens answer is structured as each
position, then **where they disagree**, then a call that names which lens it
sided with and what that cost. When fewer lenses obviously fit than your depth
allows, the extra slots are filled from opposing families, because two lenses
that agree tell you nothing a single lens would not have.

Ask *"ship it now or keep polishing?"* with Kutuzov and Patton pinned and you get
the argument you were already having with yourself, made explicit.

Generals are markdown with YAML frontmatter, and only a compact card of each one
reaches the model — never the full doctrine. Selection is deterministic keyword
routing, so it costs no extra model call. Add or replace any of them through
`GENERALS_DIR`. See [docs/generals.md](docs/generals.md), which also covers why
the Wehrmacht figures are included and how to remove them.

## Relocation planner

`/relocation` turns a stay somewhere into a checklist with real quantities and a
budget. Give it a destination, dates, housing type and climate; it produces the
things that stay actually needs, how many of each, what they roughly cost, and
the mistakes worth avoiding — filtered to the ones that apply to you.

The quantities are the point. Durable goods do not scale with the stay: you need
the same two towels for two weeks or six months. Consumables do, and rounded up
— a 50ml sunscreen applied properly is about three weeks, so a three-month stay
is a different shopping list rather than a longer one.

It also covers the things people discover too late: that "furnished" rarely
includes bedding or a sharp knife, that a dining table is the wrong height to
work at for three months, that your prescription may be a controlled substance
where you are going.

**About the prices.** The catalogue fixes *what* you need and *how many*. The
prices start as rough global anchors in USD, and **Price for this city** asks the
model to localise them. Those are estimates and the interface says so on every
line — an `est` badge, and the share of the total that is guesswork. Type a real
price on any line and it replaces the estimate, is marked as confirmed, and is
remembered for the next time you stay in that city.

Extend or correct the knowledge base without forking: point `RELOCATION_CATALOG_DIR`
at a directory with `catalog/` and `pitfalls/` YAML. An entry with an existing id
replaces the shipped one; a new id is appended. It is validated at startup, and a
broken entry fails the boot rather than silently vanishing from your checklist.

## Configuration

Copy `.env.example` to `.env`. Real environment variables take precedence.

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | `postgres://room:room@localhost:5432/decision_room?sslmode=disable` | |
| `LLM_PROVIDER` | `gemini` | `gemini` or `ollama` |
| `GEMINI_API_KEY` | — | required when provider is `gemini` |
| `GEMINI_CHAT_MODEL` | `gemini-flash-latest` | an alias, so a retired version does not break the app | |
| `GEMINI_CHAT_MODELS` | — | optional failover order, tried left to right |
| `OLLAMA_CHAT_MODEL` | `qwen3:8b` | use `qwen3:4b` if RAM is tight |
| `JOURNAL_WATCH_DIR` | `./journal` | |
| `RETRIEVAL_TOP_K` | `8` | memories passed to the model |
| `RETRIEVAL_CANDIDATES` | `30` | hybrid pool before reranking |
| `DEFAULT_TIER` | `standard` | `quick`, `standard` or `deep` |
| `MAX_TIER` | `deep` | ceiling a request cannot exceed |
| `RELOCATION_CATALOG_DIR` | *(empty)* | overlay for the relocation catalogue |
| `GENERALS_DIR` | *(empty)* | overlay for the generals roster |
| `MODES_DIR` | *(empty)* | overlay for conversation modes |
| `GEMINI_FALLBACK_MODEL` | `gemini-flash-lite-latest` | used when the main model is out of quota |
| `CHECKIN_ENABLED` | `false` | scheduled check-ins and nudges |
| `CHECKIN_SLOTS` | `morning@08:00,midday@13:00,evening@21:00` | |
| `CHECKIN_TZ` | *(machine's)* | e.g. `Asia/Almaty` |
| `COMMITMENT_EXTRACTION_ENABLED` | `true` | propose open loops from what you write |
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
| `GET` | `/api/generals` | the roster |
| `GET` | `/api/modes` | conversation modes |
| `POST` | `/api/capture` | append a line to today's note |
| `POST` | `/api/admin/reindex?max=N` | re-embed memories after switching model |
| `GET` `POST` | `/api/commitments` | open loops |
| `PATCH` | `/api/commitments/{id}` | keep, drop, done, reopen, edit |
| `GET` | `/api/checkins/unseen` | waiting check-ins |
| `POST` | `/api/checkins/now` | check in now |
| `POST` | `/api/checkins/{id}/open` | reply — opens the conversation |
| `POST` | `/api/memories` | ingest (JSON or multipart upload) |
| `GET` | `/api/memories/search?q=&kind=&tags=` | hybrid search |
| `POST` | `/api/consult` | one-shot question |
| `GET` `POST` | `/api/chat/sessions` | |
| `GET` `DELETE` | `/api/chat/sessions/{id}` | |
| `POST` | `/api/chat/sessions/{id}/messages` | |
| `GET` `POST` | `/api/relocation/plans` | |
| `GET` `PATCH` `DELETE` | `/api/relocation/plans/{id}` | |
| `POST` | `/api/relocation/plans/{id}/build?reprice=true` | rebuild, optionally repricing |
| `PATCH` `DELETE` | `/api/relocation/items/{id}` | |

```bash
curl -X POST localhost:8080/api/consult \
  -H 'Content-Type: application/json' \
  -d '{"question": "What did I decide about the database?"}'
```

## Roadmap

- [x] Hybrid retrieval, chat with history and summarisation, journal watcher
- [x] Retrieval fixes: relevance/recency rebalance, full-text query rewriting
- [x] **Depth tiers** — quick, standard and deep answers
- [x] Markdown rendering for answers
- [x] **Generals** — pick up to three strategic lenses; they argue, then synthesise
- [x] **Modes** — plan a day, make a hard call, unstick a stalled task, debrief
- [x] **Relocation planner** — setup checklist, costs and pitfalls for a stay
- [x] Deep retrieval: query decomposition, model re-ranking, doctrine lookup
- [x] Chunk breadcrumbs, overlap, and embedding-model versioning
- [x] **Commitments** — open loops tracked from your own conversations
- [x] **Check-ins** — it asks how the day is going, instead of waiting
- [ ] Streaming answers

## Privacy

With `LLM_PROVIDER=gemini`, your journal text is sent to Google when you ask a
question or ingest a file. With `ollama`, nothing leaves your machine. Postgres
runs locally either way. There is no telemetry and no account.

## Licence

MIT
