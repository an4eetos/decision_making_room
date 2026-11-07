# v3 — The field app

A roadmap for taking Decision Room off the laptop: the backend hosted on
Railway, one Flutter app for iOS and Android, and the web client kept, all in
this repository.

Decided:

- **The backend is hosted on Railway.** One Go service and one Postgres with
  pgvector per person. The phone and the browser both reach it over HTTPS.
- **Monorepo, split in two.** `backend/` is the Go server; `client/web/` is the
  browser UI that exists today; `client/flutter/` is the app.
- **Keys live on the server.** Provider keys, models, slots: entered once in a
  client, stored encrypted by the backend. The phone keeps only its session.

The server does not move to the phone. Postgres with pgvector cannot run there,
the generals council needs a process that stays up while five agents dig, and
the check-in scheduler needs a clock that does not sleep. The phone and the
browser are two clients of one JSON API, and the work splits into two halves
that run in parallel once the contract between them is written.

## What changes, in one table

| Today | After v3 |
|---|---|
| `journal/` on disk, watched by fsnotify, is the source of truth | The database is the source of truth; the folder watcher survives only for local runs |
| `about-me.md` and `context/` read from disk at startup | Read from the database on every answer; edited in a client |
| Keys and models in `.env` | Entered in a client, stored encrypted; environment variables still override |
| No auth, `localhost` only | A password and a session; HTTPS from Railway; every `/api/*` route checks |
| Check-ins appear in the browser when you open it | Local notification at each slot on the phone; the text waits on the server |
| Templates and static files embedded in the Go binary | `client/web/` is static files, copied into the image at build; `backend/` serves only JSON and those files |
| `make run` starts Docker Postgres and the server | Same, from `backend/`; `railway up` deploys |

## Repository layout

```
backend/            the Go module, unchanged inside: cmd/, internal/, migrations/
  Dockerfile        builds the server and copies client/web/dist into the image
client/
  web/              the five pages, app.js and friends, style.css, portraits
  flutter/          the app
docs/
  openapi.yaml      the contract both clients are generated or tested against
railway.toml        the service, its health check, the volume for exports
```

`go:embed` cannot reach outside the module, so the server stops embedding the
UI. Locally `WEB_ROOT=../client/web` serves it from disk, which the README
already documents as the way to edit CSS without rebuilding. In the image the
Dockerfile copies it next to the binary. The two template variables in use,
the page title and the memory kinds list, become a fetch from the API, which
is a smaller change than keeping Go templates alive for them.

## Milestones

### v3.0 — Hosted, API-first backend

No Flutter yet. The browser gets every new feature first; that is how the
backend is tested.

- [x] **Split the repository.** `git mv` into `backend/` and `client/web/`,
  the module path unchanged, `Makefile` at the root delegating into
  `backend/`. The web client is read from `WEB_ROOT`, never embedded.
- [x] **Railway.** `railway.toml` with the Go service built from
  `backend/Dockerfile`, a Postgres service from the pgvector template,
  `DATABASE_URL` injected as a reference, `/health` as the health check,
  `HTTP_ADDR` from `PORT`. Migrations already run at boot, so a deploy is a
  push. `LLM_PROVIDER=ollama` is a local-only option and the docs say so.
- [x] **Auth.** The room is on the internet now, so loopback trust is not
  enough.
  - `migrations/014_auth.sql`: `auth_owner(password_hash, created_at,
    updated_at)`, one row at most, and `auth_sessions(token_hash, client,
    created_at, last_seen_at, expires_at)`. Prefixed so `sessions` is never
    read as the chat's. The password is argon2id in PHC form; only the
    SHA-256 of a session token is stored.
  - `ROOM_PASSWORD` sets the owner on first boot, and is authoritative after:
    a different value on a later boot replaces the password and signs every
    device out, which is the recovery path. Without it, `/setup` sets the
    first password, but only with a **one-time code printed in the server
    log**: otherwise the first stranger to find a fresh Railway URL would own
    the room. `/setup` is 404 once a password exists.
  - `POST /api/auth/login` opens a session that slides 90 days forward with
    use; `DELETE /api/auth/sessions/{id}` revokes one; `GET
    /api/auth/sessions` lists devices by name; `POST /api/auth/logout`. The
    browser asks for an `HttpOnly`, `SameSite=Lax` cookie and never sees the
    token; the phone gets the token and sends it as `Authorization: Bearer`.
    A cookie-carried write must be same-origin (`Sec-Fetch-Site`, then
    `Origin`).
  - **OIDC sign-in** beside the password, for any OpenID Connect issuer
    (Google documented): authorization code with PKCE, state bound to the
    browser by a cookie, nonce checked, and `OIDC_ALLOWED_EMAILS` (verified
    addresses only) required. The app signs in through the system browser:
    `/auth/oidc/start?app_redirect=decisionroom://auth&code_challenge=…`, back
    to the app with a one-time code, redeemed at `POST
    /api/auth/oidc/exchange` with the app's own verifier (RFC 8252).
    `PASSWORD_LOGIN_ENABLED=false` leaves OIDC as the only way in.
  - `RequireAuth` in `internal/infra/http` wraps the whole mux, so a route
    is private unless it is on the public list: `/health`, `/static/`,
    `/login`, `/setup` and the sign-in endpoints. Pages redirect to `/login`;
    API calls get 401. Login, setup and the exchange are rate limited per
    address, with a global backstop, and argon2 runs at most two at a time.
  - Single owner, deliberately: one Railway project per person is the
    hosting model. Accounts and tenancy are a different product and nothing
    here prevents it later.
- [ ] **Journal store.** The folder stops being the source of truth.
  - `migrations/016_journal.sql`: `journal_documents(path, body, updated_at,
    content_hash)`, one row per markdown file, `path` unique.
  - `internal/journal/port`: a `DocumentStore` interface — `List`, `Get`,
    `Put`, `Delete`, `Changed(since)`.
  - `internal/journal/adapters/driven/postgres`: the real store.
  - The fsnotify watcher becomes a **local mirror**, on only when
    `JOURNAL_WATCH_DIR` is set: a file saved on disk is `Put` into the store,
    a `Put` through the API is written to disk. Content hash decides who is
    newer; a conflict keeps both. On Railway the variable is unset and the
    watcher never starts.
  - `SyncFile` ingests from the store, not from `os.ReadFile`.
    `service/classify.go` keys on the stored path, so `daily/…` is still a
    daily log and `context/…` is still always-in-context.
  - `Capture.Execute` appends through the store.
  - `InitialContextReader` gets a Postgres implementation reading
    `about-me.md` and `context/*` from the store.
  - Routes: `GET /api/journal` (tree with hashes), `GET /api/journal/{path...}`,
    `PUT /api/journal/{path...}` (whole document, `If-Match` on the hash),
    `DELETE /api/journal/{path...}`, `GET /api/journal/export` (zip),
    `POST /api/journal/import` (zip, for moving a folder in once).
- [x] **Settings store.** Keys stop living in `.env`.
  - `migrations/015_settings.sql` (it landed before the journal store, so it
    took 015): `settings(key, value, secret, created_at, updated_at)`.
    Secrets are sealed **in the server** with AES-256-GCM under a key derived
    from `SETTINGS_KEY` by HKDF, the setting's name as associated data, not
    with `pgcrypto`: `pgcrypto` would send `SETTINGS_KEY` to Postgres inside
    every query, where statement logging can keep it. A check constraint
    refuses an unsealed secret. Lose the key and the stored keys are gone, not
    leaked; a row sealed under another key is reported and ignored, never
    fatal.
  - `config.Load` became `config.Build(lookup)`, and `internal/infra/config`
    grew a `Resolver` that builds again with the environment (with `.env`)
    over the table over the default, so a stored value is parsed, defaulted
    and validated exactly like a variable. Only catalogue keys are ever read
    from the table. The table is loaded before any constructor receives
    `config.Config`, so settings read once at boot see stored values too.
  - A missing key no longer stops the room from starting: it is logged at
    boot and reported on the question that needs it ("add one in Settings").
  - The provider clients are unchanged; `memory/adapters/driven/aiswitch` sits
    in front of them, reads the configuration on each call, and rebuilds a
    client only when its own settings change. The embedding cache and the
    doctrine index key on the embedding model, and the index rebuilds after a
    settings change, so a key typed in later finishes what boot could not.
  - Live: provider, keys, chat and fallback models, effort, Ollama chat model,
    default and max tier, check-ins on/off and slots, sortie minutes, the
    extraction and staff toggles. `CHECKIN_TZ` is stored but applies after a
    restart: the timezone is threaded through campaign and check-ins as a
    value, and the view marks it `pending_restart`. Environment only:
    `DATABASE_URL`, `HTTP_ADDR`, `SETTINGS_KEY`, base URLs, sign-in, overlay
    dirs.
  - Routes: `GET /api/settings` (the catalogue with values, secrets masked to
    their last four characters, source, env lock, problems), `PUT
    /api/settings` (all changes validated together, one transaction, `null`
    resets), `POST /api/settings/test` (one chat call and one embedding,
    optionally with unsaved values, returning the provider's own error).
- [ ] **The web client uses the new stores.** A login page, a `/journal` page
  with a plain textarea editor, and a `/settings` page with the test button.
  Login, setup and `/settings` (drawn from the catalogue, with the test
  button and the signed-in devices) are done; `/journal` waits for the
  journal store.
  This is where "fill it in the app" first exists.
- [ ] **Contract.** `docs/openapi.yaml`, hand-written, covering every route in
  the README's API table plus the new ones. Go handler tests validate every
  response against it with `kin-openapi`, so the contract cannot drift. The
  Dart client is generated from it.
- [x] `GET /api/checkins/schedule` returns the slots and timezone so a phone
  can set local alarms. The stream route already sends a heartbeat comment
  every 15 seconds, so a carrier does not close an idle socket mid-council.

**Done when:** a fresh Railway project boots from this repository, you set a
password, log in from a browser, write `about-me.md` on `/journal`, paste a
Gemini key on `/settings`, ask a question and the answer knows who you are.
Locally, `make run` with a journal folder behaves exactly as before.

### v3.1 — Flutter foundation

- [ ] `client/flutter/`, iOS and Android targets, `flutter_lints` on, CI
  builds both on every PR that touches it.
- [ ] **Stack.** Riverpod for state, go_router for navigation, dio for HTTP,
  the generated OpenAPI client for models, flutter_secure_storage for the
  session, drift (SQLite) for the offline cache and outbox. Markdown through
  `markdown_widget`; the map through `flutter_svg`, straight from
  `/api/campaign/map.svg`.
- [ ] **Sign in.** Server address and password, or scan a QR from the web
  client's settings page that carries the address. Calls `/health`, logs in,
  names the device in the sessions list.
- [ ] **Chat.** Session list, a conversation with streamed answers over SSE
  rendered as markdown, depth under the composer, generals and modes as
  bottom sheets, the mode chip with its reason. Reconnect resumes from the
  last event id rather than re-sending the question.
- [ ] **Capture.** The first screen after sign-in is a text field and a send
  button to `/api/capture`: one tap from the home screen, a quick-action
  shortcut, and a share-sheet target for text.
- [ ] **Settings** screen over `/api/settings`, with the test button.

**Done when:** a new user installs the app, signs in to their Railway room,
enters a key in the app, captures a line and asks about it at Standard depth
with the answer streaming in.

### v3.2 — The journal in the app

- [ ] **Today.** An editor for `daily/{today}.md`, opened in one tap,
  autosaving through `PUT /api/journal` with `If-Match`. Yesterday and a
  calendar behind it.
- [ ] **About me and context.** Editors for `about-me.md` and each file under
  `context/`, with the always-in-context badge and the size the server
  reports, so you know what every answer carries.
- [ ] **Everything else.** The tree, a new-note action, rename and delete.
- [ ] **Offline outbox.** Captures, journal saves, sortie and energy taps are
  written to drift first and replayed in order when the server is reachable.
  A save that loses an `If-Match` race shows both versions and asks.
- [ ] **Ingest.** Share a file or a URL's text into `/api/memories`, and the
  memories search screen.

**Done when:** a week of daily notes written only on the phone is searchable
from the browser, and an export zip holds the same files.

### v3.3 — Loops, check-ins, supply

- [ ] **Open loops.** Proposals to keep or drop, the list, keep, done, drop,
  reopen, edit.
- [ ] **Check-ins.** Local notifications at the slots from
  `/api/checkins/schedule`, in the server's timezone. Tapping one opens the
  unseen check-in; replying opens it as a conversation. *Check in now* and
  dismiss. No push service: the server writes the check-in at the slot, the
  phone fetches it when opened, which is the rule the laptop already follows.
- [ ] **Energy** as a one-tap row under the evening check-in and on the
  campaign screen; **sorties** as a one-tap log on a front, long press for
  minutes, order and day. Both go through the outbox.
- [ ] Home-screen widgets for capture and energy once the outbox is solid; a
  widget that drops taps is worse than none.

**Done when:** a day run entirely from the phone leaves the same loops,
check-ins, sorties and energy on record as a day in the browser.

### v3.4 — The campaign on the phone

- [ ] **Map.** The SVG from the server, pinch to zoom, tap an objective or a
  front to open its sheet. The timelapse as a scrubber.
- [ ] **Intel.** Proposals, keep or drop, retype, confirm a strength, lift
  fog, send recon.
- [ ] **The staff.** Readings, *Ask the staff* with the 202-and-poll flow, the
  appraisal with *Discuss*; red team from an objective or a front.
- [ ] **Logistics and the record.** Supply per front, funds, deployments; the
  quarter from planning through operations to the after-action, predictions
  scored, calibration.
- [ ] **Beast mode and interrogation.** The orders board needs a screen that
  stays awake and a notification when an order expires. Interrogation is chat
  with a different composer.

**Done when:** every route in the README's API table has a screen in the app.

### v3.5 — Shipping

- [ ] **Relocation planner**, last because it is the least phone-shaped.
- [ ] Deep links from a check-in notification into its conversation, and from
  the map into an objective.
- [ ] Theme: `client/web/style.css` tokens become a Flutter `ThemeData` so the
  two clients look like one room. Dark mode follows the system.
- [ ] Accessibility: dynamic type, VoiceOver and TalkBack labels on every map
  element the SVG carries a title for.
- [ ] **Release.** fastlane for both stores, TestFlight and Play internal
  testing from CI on a tag, a one-click Railway template for the backend, and
  the README roadmap updated.

## Still open

- **Flutter web.** The app could also build for the browser and replace
  `client/web`. Not planned: the web client works, and two clients keep the
  API honest. Revisit once the app has caught up.
- **Hosting cost.** Railway charges for a service that is always on, and the
  check-in scheduler needs it to be. Expect a few dollars a month for the two
  services; sleeping the service is not compatible with scheduled check-ins.
- **Backups.** Railway Postgres has snapshots; the journal export zip is the
  user-facing backup. Which one the README recommends is a docs decision.

## Risks

- **It is on the internet now.** Auth, rate limiting on login and secrets at
  rest are v3.0 hard requirements, not polish. The README privacy section
  needs a paragraph on what Railway sees and what `SETTINGS_KEY` protects.
- **A long stream on a bad connection.** A deep council runs for minutes.
  Heartbeats and resume-by-event-id are the fix; the bug to test for is a
  reconnect that re-sends the question and runs the council twice.
- **Two writers on one note.** Browser and phone both editing
  `daily/today.md`. `If-Match` on the content hash and keep-both-on-conflict
  handle it; test it with the local mirror on, where there are three writers.
- **Store review.** Bring-your-own-key apps are accepted on both stores, but
  an app that does nothing until the user deploys a backend will draw a
  reviewer's question. The Railway template plus clear onboarding copy, or a
  demo room, answers it.

## Order of work

```
v3.0 split + Railway + auth + stores ──┬── v3.1 foundation ── v3.2 journal ── v3.3 loops ── v3.4 campaign ── v3.5 ship
                                       └── client/web login, /journal, /settings (tests v3.0 without a phone)
```

v3.0 has to finish first; everything on the phone depends on the session, the
journal store and the settings store. From v3.1 on, backend changes are small
additions to the contract and the app milestones run one after the other.
