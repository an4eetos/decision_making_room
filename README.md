# Decision Room

A private thinking partner that remembers. Write markdown into a folder; ask it
questions about your own decisions, plans and days. Everything runs on your
machine — one Go binary and a Postgres container.

It is built around a simple idea: an assistant that has read everything you have
written about your own work gives better advice than one that has not.

> **Status:** early but usable. Retrieval, chat with depth tiers, the generals
> roster, conversation modes, interrogation, beast mode, open loops, check-ins, the journal
> watcher and the relocation planner all work today, and v2, **the Campaign**,
> is under way: objectives, opposition and fog of war proposed from what you
> write, drawn as a theatre map, read by the staff — the generals appraise
> the map, check-ins are sitreps, a red team attacks your plan — and now
> supplied: every front's orders are weighed against the hours, energy and
> money you report — and kept as a record: a quarter runs from planning
> through operations to an after-action, pre-mortem predictions are scored
> by you, and calibration is the one number. See [the roadmap](#roadmap).

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

Want Claude to answer? Set `LLM_PROVIDER=anthropic` and `ANTHROPIC_API_KEY`.
Anthropic has no embeddings API, so the journal is still embedded by Gemini
(keep `GEMINI_API_KEY`) or, with `EMBED_PROVIDER=ollama`, locally.

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
| **Deep** | question split into facets, pool of 60, model-reranked | each general digs on its own, up to two rounds | a decision worth the wait |

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

Deep also seats the generals as a **council**: each one runs as its own agent,
reads your notes and argues its position alone, and one closing call writes the
exchange between them. That costs one call per general plus one, and the
answer waits for the slowest general. See [docs/generals.md](docs/generals.md).

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
framework. Four more are never detected and only ever entered on purpose:
**Interrogation**, **Beast mode**, and the two staff modes, **Staff appraisal**
and **Red team**.

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
- With a campaign map they become **sitreps**, drawn from map state: **morning
  orders** name today's point of main effort and the order that serves it, the
  **midday contact report** says which orders are in contact and whether you are
  on them, and **evening gains and losses** reports what moved on the map today
  and what did not. With no map, a check-in is what it always was.
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

## Campaign

The **Campaign** page holds what the room knows about your goals. You open a few
**fronts** — areas of life and work — and on them sit **objectives** (positions
to take), **opposition** (what is dug in between you and them) and **fog** (what
you do not know yet).

Write *"I want to move to Lisbon by March, but the bank still hasn't sent the
statement the visa needs, and I'm not sure if the visa takes six weeks or six
months"* and three proposals appear, here and in the chat's left rail: an
objective, an obstacle typed *waiting on input* and linked to it, and an unknown.
Nothing goes on the map until you keep it.

- **Opposition** is typed by the five kinds of stuck from the Stalled mode, each
  with its treatment, and given a strength from outpost to fortress. A strength
  the room estimated is marked `est` until you confirm it.
- **Open loops become orders** when aimed at something on the map. An order aimed
  at fog is **reconnaissance**: finishing it asks what you found, and the answer
  lifts the fog. *Send recon* on an unknown writes the loop for you.
- **Interrogations feed it for free**: what an interrogation lists as still dark
  becomes proposed fog, and its probe becomes a proposed order aimed at what it
  goes after.
- **Withdrawing** an objective is a legitimate order, not a loss.

An ordinary turn costs one background model call, and only when what you wrote
sounds like a goal, a blocker or an open question; a free prefilter skips the
rest. If you never open the page, the room answers exactly as before. Details in
[docs/campaign.md](docs/campaign.md).

### The staff

Once there is a map, the generals read it with you. Every answer carries the
map in its context — the fronts, what sits on them, what moved in the last day,
and what the arithmetic says — so an answer about this week's work is written by
lenses that can see ◆ *Ship v2* is overdue and ▲ *No tests* is a fortress with
nothing aimed at it. No model call is spent on this; it is one database read.

- **Readings** are what the numbers say on their own: an objective past its
  date, a front overextended (orders open, nothing moved) or gone quiet, a
  fortress with nothing aimed at it, fog nobody has scouted, strengths still
  estimated, the objective closest to taken. They refresh on every load of the
  campaign page.
- **Ask the staff** has the generals appraise the map: one point of main effort,
  where to yield ground, what is overextended, which fog to lift first, and two
  or three orders. Standard seats two generals in one call; Deep convenes three
  as a council, each reading the map alone. The appraisal is kept with its date,
  and **Discuss** opens it as a conversation with the generals who wrote it
  seated.
- **Red team**: one general takes command of the opposition on your map and
  attacks your plan — how each position holds, where the plan breaks, which fog
  it exploits, what it concedes, and what it would take to beat it. Open it from
  an objective or a front on the campaign page, from the briefing bar, or from
  the mode menu. The attacks it names arrive in Intel as proposed opposition and
  the fog it exploits as proposed unknowns, with no extra model call.

Withdrawal is a legitimate order, and the staff will recommend it when it is the
right one. Set `CAMPAIGN_STAFF_ENABLED=false` to keep the map out of ordinary
answers; the readings, the appraisal and the red team still work.

### Logistics

A front with more orders than supply is overextended, so the campaign page
keeps a supply line for each front, and every figure in it is one you
reported — none is read from what you write:

- **Time** comes from **sorties**, the focus blocks you log on a front. One
  tap logs a default block today; or give it a length, the order it served,
  and a day you forgot to log.
- **Energy** is one tap a day, 1 *spent* to 5 *sharp*, under the evening
  check-in or on the campaign page.
- **Money** is the funds you set against a front, against what its objectives
  cost and what the stays deployed on it will cost. Different currencies are
  listed side by side, never converted at a guessed rate.

The staff reads it before a front collapses rather than after the line stops
moving: **outrunning its supply** when what falls due this week needs more
than the front's hours a week plus one spare sortie, or its backlog would
outlast the two weeks an order stays fresh; **supply cut** when the sorties
stopped while orders fall due; **overdrawn** when costs pass funds; a
**deployment** landing within two weeks with critical pitfalls unhandled;
**low energy** when most recent evenings were. An order costs one sortie,
marked `est`, until the front has done three and has a rate of its own. A
front you never log sorties on is never called out of supply, and with
nothing reported, answers are exactly what they were. Details in
[docs/campaign.md](docs/campaign.md#logistics).

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

**Deployed on** puts a stay on a campaign front as a forward base: its setup
budget draws on the front's funds, still marked with how much of it is an
estimate, and the staff reads its readiness — critical pitfalls unhandled,
items still needed — in the two weeks before it lands.

Extend or correct the knowledge base without forking: point `RELOCATION_CATALOG_DIR`
at a directory with `catalog/` and `pitfalls/` YAML. An entry with an existing id
replaces the shipped one; a new id is appended. It is validated at startup, and a
broken entry fails the boot rather than silently vanishing from your checklist.

## Configuration

Copy `.env.example` to `.env`. Real environment variables take precedence.

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | `postgres://room:room@localhost:5432/decision_room?sslmode=disable` | |
| `LLM_PROVIDER` | `gemini` | `gemini`, `anthropic` or `ollama` |
| `EMBED_PROVIDER` | same as `LLM_PROVIDER`; `gemini` for `anthropic` | `gemini` or `ollama`; switching means a reindex |
| `GEMINI_API_KEY` | — | required when `gemini` answers or embeds |
| `GEMINI_CHAT_MODEL` | `gemini-flash-latest` | an alias, so a retired version does not break the app | |
| `GEMINI_CHAT_MODELS` | — | optional failover order, tried left to right |
| `ANTHROPIC_API_KEY` | — | required when provider is `anthropic` |
| `ANTHROPIC_CHAT_MODEL` | `claude-opus-5-5` | `claude-sonnet-5-5` or `claude-haiku-5-5` cost less |
| `ANTHROPIC_FALLBACK_MODEL` | `claude-sonnet-5-5` | used when the main model stays overloaded; empty disables |
| `ANTHROPIC_CHAT_MODELS` | — | optional failover order, tried left to right |
| `ANTHROPIC_EFFORT` | `medium` | `low` to `max`; empty sends none |
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
| `CAMPAIGN_EXTRACTION_ENABLED` | `true` | propose objectives, obstacles and unknowns from what you write |
| `CAMPAIGN_STAFF_ENABLED` | `true` | read the campaign map into every answer, so the generals appraise the map and not just the question |
| `SORTIE_MINUTES` | `25` | the default sortie (focus block), and what an order is estimated to cost before a front has a rate of its own |
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
internal/infra        config, postgres, gemini, anthropic, ollama, http
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
| `GET` | `/api/campaign` | fronts, items and the orders aimed at them |
| `GET` | `/api/campaign/proposals` | items waiting for keep or drop |
| `POST` | `/api/campaign/fronts` | open a front; `/fronts/starters` opens the starter set |
| `PATCH` | `/api/campaign/fronts/{id}` | rename, withdraw, restore |
| `POST` | `/api/campaign/items` | add an objective, obstacle or unknown |
| `PATCH` | `/api/campaign/items/{id}` | keep, drop, resolve, withdraw, file, retype, confirm strength, cost an objective |
| `POST` | `/api/campaign/items/{id}/lift` | lift fog with what you found |
| `POST` | `/api/campaign/items/{id}/recon` | send a recon order against an unknown |
| `PUT` | `/api/campaign/orders/{id}/target` | aim an open loop at an item |
| `GET` | `/api/campaign/map.svg?day=` | the theatre map, today's or a past day's; `/theatre` is the same as JSON, `/timelapse` the days on record |
| `GET` | `/api/campaign/sitrep` | the map as the staff reads it: lines, orders by name, what moved, the readings |
| `GET` `POST` | `/api/campaign/appraisals` | what the staff said, latest first, with whether one is being written; `POST {"front_id", "tier"}` starts one in the background and returns 202 |
| `POST` | `/api/campaign/appraisals/{id}/open` | discuss — opens the conversation |
| `GET` | `/api/campaign/logistics` | supply per front, energy, deployments, and the last two weeks of sorties |
| `GET` `POST` | `/api/campaign/sorties` | focus blocks flown; `POST {"front_id", "order_id", "minutes", "day", "note"}`, all but the front optional |
| `DELETE` | `/api/campaign/sorties/{id}` | take a sortie off the record |
| `GET` `PUT` | `/api/campaign/energy` | the week's taps; `PUT {"day", "level"}` is the one tap, 1 to 5 |
| `DELETE` | `/api/campaign/energy/{day}` | take a day's tap back |
| `GET` | `/api/campaign/fronts` | the fronts alone; `PATCH /fronts/{id}` also takes `funds` and `currency`, or `clear_funds` |
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
| `PUT` | `/api/relocation/plans/{id}/deployment` | deploy on a campaign front, `{"front_id"}`; empty takes it off the map |
| `PATCH` `DELETE` | `/api/relocation/items/{id}` | |

```bash
curl -X POST localhost:8080/api/consult \
  -H 'Content-Type: application/json' \
  -d '{"question": "What did I decide about the database?"}'
```

## Roadmap

### v1 — shipped

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

Streaming answers did not make v1 and opens v2.

### v2 — The Campaign

v1 is a set of good tools that do not see each other. A commitment does not know
which goal it serves, a check-in does not know what is blocking you, and no
screen shows the whole situation at once.

v2 draws it as a theatre map. Your goals are objectives to take. Your problems
are the opposition, dug in between you and them. Your commitments are orders,
your time and energy are supply, and the twenty-one generals stop being lenses you
pick and become a staff that reads the map with you. The map moves as you work:
close a loop and the front line advances; let a front go quiet and it shows.

The map is a view of what you already wrote, not a second app to maintain. If
you never open it, the room answers exactly as it does today.

| In your life | On the map |
|---|---|
| Goals | **Objectives** — positions to take, proposed from what you write, kept only if you agree |
| Problems, blockers, fears | **Opposition** — enemy positions, typed by the five kinds of stuck from the Stalled mode |
| What you do not know yet | **Fog of war** — lifted by reconnaissance: a question answered, a small test run |
| Areas of life and work | **Fronts** |
| Commitments | **Orders** — assigned to a front, aimed at an objective or an enemy position |
| Focus blocks | **Sorties** |
| Time, energy, money, attention | **Supply** — a front with more orders than supply is overextended |
| Stale loops | **Units out of contact** — "gone quiet" asks: reinforce or withdraw? |
| Dropping a goal | **Orderly withdrawal** — a legitimate order, not a loss |
| Generals | **The staff** — each reads the map through its own doctrine and its declared blind spot |
| Modes | **Operation types** — planning, decision, breakthrough, after-action |
| Check-ins | **Sitreps** — morning orders, midday contact report, evening gains and losses |
| Debriefs and pre-mortems | **After-action reports**, and predictions that get scored later |
| Relocation planner | **Deployment to a new theatre** — logistics for a forward base |
| Journal | **Field dispatches** — the map's only source of truth |

**Rules of engagement.** Gamification usually rewards activity and punishes rest,
which is how productivity apps end up nagging people into lying to them. The
campaign keeps the rules v1 already follows:

- Nothing goes on the map without your agreement — the same rule as open loops.
- Intelligence is labelled. An enemy's strength, a front's cost, a supply estimate
  is marked `est` until something tests it, exactly as relocation prices are.
- Outcomes and calibration are scored, never activity. No streaks, no XP, no
  penalty for a day off.
- Withdrawal is a valid order, and the staff will recommend it when it is the
  right one. Kutuzov gave up Moscow.
- Every layer degrades to v1. No map data means no map, not a worse answer.
- Deterministic where it can be, a model call only where judgment is needed.

**Milestones**

- [x] **v2.0 Groundwork** — streaming answers; fronts; objectives as proposals you
  keep or drop; commitments linked to the objective they serve
- [x] **v2.1 Opposition and intel** — obstacles extracted from what you write,
  typed by kind of stuck, strength estimated then confirmed; fog of war over
  what is unknown; reconnaissance as a kind of order
- [x] **v2.2 The theatre map** — server-rendered SVG of fronts, objectives and the
  opposition, with a front line that moves as loops close; a daily snapshot and a
  timelapse of the campaign so far
- [x] **v2.3 The staff** — generals appraise the map, not just the question: where
  the point of main effort is, where to yield ground, what is overextended.
  Check-ins become sitreps drawn from map state. **Red team**: one general takes
  command of the opposition and attacks your plan
- [x] **v2.4 Logistics** — supply per front: hours from your sorties, energy you
  report in one tap at the evening check-in (never inferred from your writing),
  money; overextension warnings before a front collapses; relocation as a
  deployment with its own supply line
- [x] **v2.5 Campaign record** — campaigns by quarter, from planning through
  operations to an after-action review; pre-mortem predictions scored at the
  debrief, with calibration as the one number worth watching; commendations for
  what actually matters — an objective taken, an honest withdrawal, a pre-mortem
  that called it

## Privacy

With `LLM_PROVIDER=gemini`, your journal text is sent to Google when you ask a
question or ingest a file. With `anthropic`, what goes into a question is sent
to Anthropic, and the text being embedded still goes to Google unless
`EMBED_PROVIDER=ollama`. With `ollama` for both, nothing leaves your machine. Postgres
runs locally either way. There is no telemetry and no account.

## Licence

MIT
