# Architecture

How Runsten is put together: what runs, where the code for each part lives, and the few rules
that hold it together. Read it before your first pull request; read [`AGENTS.md`](AGENTS.md)
for the detailed rules of each part, and [`DEVELOPMENT.md`](DEVELOPMENT.md) to run it.

## Bird's eye view

Runsten polls the Volvo Cars API for the vehicles of its users, stores every raw response,
derives trips and charging sessions from them, and serves the result through a JSON API and a
web interface.

```
                 Volvo ID (OAuth)        Volvo Cars API
                        │                      │
                        │ tokens               │ raw JSON, polled
                        ▼                      ▼
 browser ──► runsten-web ──► runsten-api   runsten-collector ──► MQTT broker
             (files, CSP,    (sessions,     (adaptive polling,     (state, Home
              relay)          JSON API,      quota budget,          Assistant
                              Volvo ID       derivation)            discovery)
                              connection)        │
                                   │             │
                                   ▼             ▼
                              PostgreSQL ── snapshots (raw, source of truth)
                              (RLS per       trips, charges (derived, rebuildable)
                               account)      accounts, users, places, tariffs…
```

Three ideas shape everything else:

1. **Raw snapshots are the source of truth.** The collector stores each API response as it
   came. Trips and charges are derived from them, and can be thrown away and rebuilt at any
   time (`runsten-collector rebuild`).
2. **Times are bounds, not instants.** Polling only brackets a transition: a trip started
   between `started_after` and `started_before`. An event the collector never saw (an outage,
   a quota run out) is reconstructed from the odometer or the state of charge, and marked so.
   Unknown is `null`, never zero, from the database to the screen.
3. **One account, one driver, isolated by the database.** Every row carries `account_id`, and
   PostgreSQL's row-level security keeps each account to its own rows.

## The binaries

All Go, one module (`runsten`), one Docker image each, under `cmd/`:

| Binary | Role |
|---|---|
| `runsten-collector` | Polls the Volvo API for every account, stores snapshots, derives trips and charges after each pass, publishes to MQTT. Subcommands: `rebuild`, `connect`, `healthcheck`. |
| `runsten-api` | Sign-in, sessions and access tokens, the Volvo ID connection (OAuth with PKCE), the JSON API `/api/v1`, health. Also runs the reverse-geocoding worker. Subcommands: `user create`, `user password`, `healthcheck`. |
| `runsten-web` | Serves the built front end with a strict CSP, and relays `/api/` and `/auth/` to `runsten-api` at the same origin. Holds no session, database or secret. |
| `runsten-simulator` | A fake Volvo API and Volvo ID driven by YAML scenarios, with quotas, outages and token expiry. For development and tests only: nobody needs a car to work on Runsten. |

The collector and the API never talk to each other: they meet in PostgreSQL. Each binary
migrates the database at startup (embedded migrations, under an advisory lock).

## Code map

Everything Go is under `internal/`. The packages fall in three rings; dependencies only point
inward.

**Domain (pure, no I/O)**

- `internal/core`: normalized snapshots, trip and charge detection (`core.Derive`), the current
  state (`core.Latest`), totals of a period (`core.Summarize`), the costs. Most of the
  interesting logic is here, and it is tested without a database or a network.
- `internal/catalog`: the data sheet of the electric Volvo and Polestar variants
  (`variants.yaml`, embedded) and how a vehicle's variant is recognized.
- `internal/publish`: what is published of a vehicle to MQTT, and Home Assistant's discovery.

**Use cases (declare the interfaces they need)**

- `internal/collector`: the polling loop: modes (parked, driving, charging), intervals, the
  quota budget, error handling (quota, refused key, 401, 429, lost grant).
- `internal/derive`: loads snapshots, runs `core.Derive`, saves the events; incremental from a
  cursor, or a full rebuild that must give the same result.
- `internal/oauth`: the Volvo ID token lifecycle: refresh, rotation, re-authentication.
- `internal/auth`: users (argon2id), sessions, login throttling, personal access tokens.
- `internal/geocode`: turns positions into addresses, when the instance names a geocoder.

**Adapters (talk to the outside world)**

- `internal/volvo`: the Volvo API and Volvo ID clients. Vendor JSON is turned into
  `core.Snapshot` here, never in `core`: a Polestar source would produce the same types.
- `internal/store`: PostgreSQL (pgx): every table, the row-level security, the migrations
  (`internal/store/migrations/NNNN_name.sql`).
- `internal/api`: the HTTP interface of `runsten-api`, its JSON API built with
  [huma](https://huma.rocks). The OpenAPI document `api/openapi.yaml` is generated from it.
- `internal/web`: the HTTP handler of `runsten-web`.
- `internal/publish/mqtt`: the MQTT connections (paho).
- `internal/platform`: generic building blocks with no business code: clock, health, HTTP
  server, `netguard`, `secretbox` (AES-256-GCM for the tokens and keys stored).
- `internal/simulator/...`: the simulator's vehicle model, scenario engine and fake API.
- `internal/debugui`: a developer's debug page of the collector (loopback only).

The binaries in `cmd/` wire them: they read the environment, build the adapters, and inject
them into the use cases through small interfaces each consumer declares itself (for instance
`collector.API`, `collector.Store`, `derive.Store`). A use case never imports `store` or
`net/http`.

**Front end** (`web/`): Vue 3, TypeScript strict, Vite, Vuetify, TanStack Query, organized by
[Feature-Sliced Design](https://feature-sliced.design):

```
web/src/
  app/        bootstrap, router, plugins, global styles
  pages/      one folder per route
  widgets/    composite blocks (the app shell…)
  features/   user actions and their data fetching (browse-trips, edit-place, manage-mqtt…)
  entities/   business objects: their types, API functions and display (trip, charge, vehicle…)
  shared/     api client, i18n, formatting (shared/lib), theme, generic UI
```

A layer imports only the layers below it, and a slice only through its `index.ts`; ESLint
enforces both. The API types are generated from `api/openapi.yaml`, never written by hand.

**Elsewhere**

| Path | What |
|---|---|
| `api/openapi.yaml` | the API contract, generated (`go test ./internal/api -update`) |
| `scenarios/` | the simulator's scenarios |
| `testdata/` | recorded Volvo API responses, used as fixtures |
| `documentation/` | the user documentation, published on the site |
| `compose.yaml`, `.env.example` | the self-hosted stack and its configuration |
| `Taskfile.yml`, `Tiltfile` | every command of development and CI; the development stack |

## A collector pass, end to end

1. Every tick (10 s by default), the collector lists the vehicles due for a call, across the
   accounts, fairest first under the quota budget.
2. For each, it gets an access token from `oauth` (refreshing it under a row lock if needed),
   and calls the endpoints that are due: often while driving or charging, rarely while parked.
3. Each response is stored as a snapshot. A response identical to the previous one (by a hash
   of its canonical JSON) only moves its `checked_at` forward: a value holds from `fetched_at`
   to `checked_at`.
4. `derive` reads the new snapshots from its cursor, `core.Derive` detects the trips and
   charges, and the events after the settled point are replaced.
5. If the account has an MQTT broker, the vehicle's state is published to it.
6. The collector writes its status per vehicle (`collector_status`), which the Connection page
   of the web interface shows.

A read of the web interface is then: browser → `runsten-web` → `runsten-api` (session from the
cookie, account from the session) → `store` (in the account's transaction, under RLS) → JSON.
Statistics are computed on each request from the derived tables: there is no aggregate table
to keep in step.

## Rules that hold it together

These are checked by tools, not by goodwill; a pull request that breaks one fails CI.

- **The import graph** is an allowlist (`internal/archtest`) and a set of depguard rules
  (`.golangci.yml`). A new package or dependency updates both.
- **Time is injected.** `time.Now` is forbidden outside `internal/platform`: take a
  `clock.Clock`. Tests use a manual clock, the simulator an accelerated one.
- **Every table is isolated.** `TestEveryTableIsIsolated` fails on a table without row-level
  security; the store always works through `store.inAccount`.
- **Derivation is reproducible.** Incremental and full rebuild must agree
  (`TestIncrementalMatchesRebuild`).
- **The API contract is visible.** Golden responses (`internal/api/testdata/`) and the
  generated OpenAPI file change in the diff of any API change, and every response of the tests
  is validated against the spec.
- **The front end is accessible.** axe runs on every view in the browser tests, with no rule
  disabled; every text is translated in English, French and Swedish.
- **No secrets in the repository.** Tokens and keys are encrypted at rest and never logged;
  Trivy scans the tree for secrets.

## Extension points

Runsten is open core: the public build is complete on its own. A few explicit hooks let another
build add modules without changing this code: `extensions.go` in `cmd/runsten-api` and
`cmd/runsten-collector` (a no-op here, replaced by files under the `premium` build tag), and the
front end's `extensions.ts` files. They exist only for a feature that uses them, and are kept
few and small. No `init()` registry, no plugin loading.

Integrations outside the process (MQTT today, the JSON API with personal access tokens) are the
way to extend Runsten in another language, under any licence.

## Where to start reading

- The domain: `internal/core/derive.go` and its tests, with a scenario of `scenarios/`.
- The collector: `internal/collector`, and `playDay` in its tests, which drives the simulator
  and the collector on one manual clock.
- The API: `internal/api/huma.go`, then one operation and its golden file.
- The front end: `web/src/app/routes/`, then one page down to its features and entities.
