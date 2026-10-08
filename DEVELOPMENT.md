# Development

How to set up Runsten, run it against the simulator, test it, and make the usual kinds of
change. [`ARCHITECTURE.md`](ARCHITECTURE.md) explains how the code is organized;
[`AGENTS.md`](AGENTS.md) holds the detailed rules of each part, for humans and AI assistants
alike; [`CONTRIBUTING.md`](CONTRIBUTING.md) covers issues and pull requests.

You do not need a Volvo: a simulator plays the Volvo API and Volvo ID from scenarios.

## Requirements

- Go, the version of `go.mod`.
- Node, the version of `web/.nvmrc` (`nvm use` in `web/`). Another version changes the ICU data,
  and two tests of the Swedish formats fail.
- Docker: PostgreSQL, Trivy, Redocly and Playwright run in containers, pinned by digest.
- [Task](https://taskfile.dev) (`brew install go-task`), or the pinned one with
  `go tool -modfile=tools/go.mod task`.
- [Tilt](https://tilt.dev) (`brew install tilt`) for the development stack, and curl; jq for
  `task compose-test`.

Everything else (golangci-lint, govulncheck, the front end's dependencies) is pinned in the
repository: nothing more to install.

## First run

```sh
cd web && nvm use && cd ..
task web-install     # npm ci
tilt up              # PostgreSQL, simulator, runsten-api, collector, Vite, Mosquitto
```

Then open http://127.0.0.1:5173 and sign in as `admin` / `runsten-dev-password`. Tilt creates
the user and connects the simulated Volvo ID on its own; its page, http://localhost:10350, has
each service's logs. A service restarts when its Go files change; Vite reloads the front end.
Ctrl-C stops everything.

The simulator runs at ×60, and so does the collector's clock in development: a simulated day
passes in 24 minutes. To play another scenario of [`scenarios/`](scenarios/):

```sh
tilt up -- --scenario=missed-trip     # or, while it runs: tilt args -- --scenario=ex30
tilt up -- --scenario=errand,ex30     # two vehicles under one Volvo ID
```

| Scenario | What it shows |
|---|---|
| `commute` | a typical day: home, work, home, then an AC charge at home (the default) |
| `errand` | a trip revealed by the odometer during an outage, a DC charge, a trip back (the browser tests use it) |
| `long-trip-dc` | a long trip with a DC charge, without the charging power (as an EX90 reports) |
| `missed-trip` | the API down for the whole way out: a trip never seen, revealed by the odometer |
| `quota-exhausted` | a quota of 150 calls a day, run out in the morning |
| `token-lifecycle` | three days with short-lived tokens, a rotation and a grant that ends |
| `ex30` | a real model the catalog recognizes, with its net capacity and onboard charger |
| `battery` | an EX30 driving and charging six times: the battery page estimates the capacity |

Each scenario has its own VIN: switching adds its vehicle next to the previous ones, which keep
their history. To start from an empty database: stop Tilt, `task db-down`, `tilt up` again.
Several scenarios are as many vehicles under one Volvo ID; only the first may have an `api`
section, whose limits apply to them all. A scenario's `vehicle` takes `usableKWh`, the capacity
its state of charge runs over (default `batteryKWh`, the nominal capacity the API reports), and
`fadePerYear`, the share of it lost per simulated year.

### Without Tilt

The same services, one terminal each:

```sh
task run-sim [SCENARIO=missed-trip]   # simulator on :8080
task dev-user                         # once: the admin user
task run-api                          # runsten-api on http://127.0.0.1:8081
task connect-sim                      # sign in and connect the simulated Volvo ID
task run-collector                    # collector; debug page on http://127.0.0.1:8090
task run-web                          # Vite on http://127.0.0.1:5173
task run-mqtt && task dev-mqtt        # optional: Mosquitto, set as the user's broker
task mqtt-sub                         # print what the collector publishes
```

### As a self-hosted instance

The Compose stack, built from your tree, against the simulator (`sim.env`, a separate Compose
project with its own volume):

```sh
task compose-sim && task connect-sim  # then http://127.0.0.1:8082
task compose-sim-down                 # stop it and remove its data
```

In a browser, the simulated Volvo ID lives at `http://simulator:8080`: add
`127.0.0.1 simulator` to `/etc/hosts` to follow its redirect.

Unlike `tilt up`, this collector runs in real time while the simulator runs at ×60: an access
token announced for 30 minutes expires after 30 seconds and is renewed after the 401, and the
energy of a charge measured from its power is 60 times too small (the battery page shows no
estimate).

The browser tests (Playwright and axe, `web/e2e/`) run alone against this stack, on the
scenario they expect:

```sh
RUNSTEN_SIM_SCENARIO=errand task compose-sim && task connect-sim
task e2e              # once its reconstructed trip is there, about three minutes;
                      # screenshots, report and traces in web/e2e-results/
```

### Looking inside

- The collector's debug page, http://127.0.0.1:8090: its calls, each vehicle's state and mode.
- The simulator's progress: `curl -s localhost:8080/sim/status`.
- The database: `docker exec -it runsten-postgres psql -U postgres runsten` (also on
  `localhost:55432`, password `runsten`). Trips and charges are derived:
  `go run ./cmd/runsten-collector rebuild` recomputes them from the snapshots.
- The API: its contract is `api/openapi.yaml`, generated from the code (never edit it by hand:
  `go test ./internal/api -update`). Render it with Redocly CLI, into `api/api.html` (ignored by
  git):
  ```sh
  docker run --rm --user "$(id -u):$(id -g)" -v "$PWD/api:/spec" -w /spec redocly/cli build-docs openapi.yaml -o api.html
  ```

## Tests

The CI runs `task ci` in three levels; each can be run locally.

| Command | What | Needs |
|---|---|---|
| `task unit` | format, lint, unit tests, vulnerabilities, build, for Go (`unit-go`) and the front end (`unit-web`) | Go, Node |
| `task integration` | the tests with PostgreSQL (`-race`, coverage ≥ 70 %), the OpenAPI lint, Trivy | Docker |
| `task system` | the images for amd64 and arm64, then the Compose stack end to end with the browser tests | Docker, ports 8080–8082, 8090, 1883 free; over 10 minutes |

While working, run what you touch:

```sh
go test ./internal/core/...           # a Go package
task test                             # all Go tests, with a throwaway PostgreSQL per test
task lint                             # golangci-lint: must report 0 issues
task web-test                         # Vitest (or npx vitest run test/path in web/)
task web-lint                         # vue-tsc, ESLint, Prettier
task web-audit / web-build            # npm audit; the production build into web/dist
task fmt                              # format Go and web/
task                                  # list every task
```

Before opening a pull request, `task unit` must pass. Run `task integration` when you touch the
store or a migration, and `task system` when you touch `compose.yaml`, the `Dockerfile` or
`scripts/`. The CI runs all three on the pull request anyway.

How tests are written here:

- Go: table-driven and deterministic: a `clock.Manual`, injected randomness. The collector is
  tested end to end against the simulator handler on a shared clock (`playDay` in
  `internal/collector/collector_test.go`).
- The API: golden responses in `internal/api/testdata/`, and every response validated against
  the OpenAPI document.
- The front end: Vitest in `web/test/`, mirroring `src/`, with the API's golden files as
  fixtures; Playwright and axe in `web/e2e/`, on desktop and phone, light and dark.

## Common changes

### An API endpoint

1. Add the operation in `internal/api` (`huma.Register`), with documented input and output
   types (`doc`, constraints and enumerations in their tags).
2. If it needs data, declare the method on the interface `api` consumes, implement it in
   `internal/store`, and wire it in `cmd/runsten-api`.
3. Test it through `checkedAPI`, then `go test ./internal/api -update`: it writes the golden
   files and `api/openapi.yaml`. Review that diff as a change of the public contract.
4. In the front end, `npm run gen` (run by every `npm run` target) gives the new types.

### A table or a column

- A new migration, `internal/store/migrations/NNNN_name.sql`. Never edit one already merged.
- A new table carries `account_id`, composite foreign keys `(account_id, id)` and an
  `account_isolation` policy, and gets an isolation test (see `TestAccountIsolation`).
- Add it to the account's export (`exportTables`) or say why not (`notExported`);
  `TestExportCoversEveryTable` checks it.
- Secrets go in `bytea` columns, sealed with `secretbox`, never exported or logged.

### A front end feature

- A slice in `web/src/features/<verb-noun>/` with its `index.ts`; TanStack Query lives in its
  `composables/`, its query keys in the entity's `model/queryKeys.ts`.
- Every visible text in `shared/i18n/locales/` in English, French and Swedish, with the same keys. If you
  do not speak French or Swedish, write your best attempt and say so in the pull request.
- Colors, fonts and radii come from the theme (`var(--v-…)`), never hard-coded.
- A new view gets a `checkView` in `web/e2e/` (axe, CSP).

### A vehicle variant

A pull request on `internal/catalog/variants.yaml`, with a source for every figure: see
[Contributing a variant](documentation/contributing/variants.md).

### A simulator scenario

A YAML file in `scenarios/`, after the existing ones. Useful to reproduce a behavior of the
real API: say in the pull request what you observed and where.

### A dependency

Avoid it if the standard library or an existing dependency does the job. Otherwise, justify it
in the commit message, and add it to the import allowlist of `internal/archtest` and, if it is
constrained, to depguard in `.golangci.yml`.

## Releases

A release is a tag `vX.Y.Z` ([semantic versioning](https://semver.org); `vX.Y.Z-rc.N` for a
pre-release) on a commit of `main` whose `ci` run passed, pushed by the maintainer:

```sh
git switch main && git pull
git tag -a v0.4.0 -m "Runsten 0.4.0" && git push origin v0.4.0
```

The workflow `release` publishes the images and creates the GitHub Release, its notes
generated from the pull requests merged since the previous tag, with the images' digests. A
change that asks something of those who update (a rebuild, a new variable) goes into the
[update notes](documentation/self-hosting/updating.md#update-notes) with its pull request,
and the notes point to it.

## Conventions in short

The full list is in [`AGENTS.md`](AGENTS.md); these are the ones a first pull request meets.

- English everywhere: code, comments, logs, commit messages, documentation.
- Match the surrounding code. Comments explain why, not what.
- Errors are wrapped with context: `fmt.Errorf("reading snapshots: %w", err)`.
- Logs go through `log/slog`, never `fmt.Print*`. No token, key, VIN or position in a log.
- Every I/O takes a `context.Context`; no global state, no `init()` with side effects.
- Do not invent Volvo API behavior. An assumption is written as such in a comment, and a named
  parameter if it drives logic.
- Never commit a real token, API key or VIN, even in a fixture.
