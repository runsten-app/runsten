# Runsten

> Open source data logger for Volvo and Polestar EVs.

Runsten records the trips, charging sessions and software updates of Volvo and Polestar electric vehicles, and presents them as a history and dashboards.
The name comes from runestones (*runsten* in Swedish), which often commemorated journeys: Runsten carves your car's trips in stone.
It is self-hostable with Docker Compose ([Self-hosting](#self-hosting)); a hosted version will also be available for those who don't want to run their own server.

Status: **early development**. A simulator of the Volvo API makes it possible to work without a vehicle. The collector stores raw API responses in PostgreSQL and derives trips and charges from them, including those it never saw (an outage, an exhausted quota): these are marked as reconstructed. A Volvo ID is connected once through OAuth; the collector then refreshes its tokens on its own. A catalog of the electric variants recognizes each vehicle's battery and onboard charger ([Vehicle models](#vehicle-models)). `runsten-api` authenticates its user and serves the current state of the vehicles, the history of their trips and charges with the cost of each charge, and their statistics; it writes the account's currency, its places with their tariffs, and the costs entered for charges ([API](#api)); `runsten-web` serves the web interface that shows them ([Web interface](#web-interface)).

## Development

[`DEVELOPMENT.md`](DEVELOPMENT.md) explains how to set Runsten up, run it against the Volvo API simulator (no car needed) and test it; [`ARCHITECTURE.md`](ARCHITECTURE.md) how the code is organized; [`CONTRIBUTING.md`](CONTRIBUTING.md) how to propose a change. In short:

```sh
task web-install     # once: the front end's dependencies (Node of web/.nvmrc)
tilt up              # the whole stack on the simulator: http://127.0.0.1:5173 (admin / runsten-dev-password)
task unit            # format, lint, tests and build, before a pull request
```

### Releases

A release is a tag `vX.Y.Z` ([semantic versioning](https://semver.org); `vX.Y.Z-rc.N` for a pre-release) on a commit of `main` whose `ci` run passed:

```sh
git switch main && git pull
git tag -a v0.4.0 -m "Runsten 0.4.0" && git push origin v0.4.0
```

The workflow `release` publishes the images ([Updating](#updating)) and creates the GitHub Release, its notes generated from the pull requests merged since the previous tag, with the images' digests. A change that asks something of those who update (a rebuild, a new variable) goes into [Updating](#updating) with its pull request, and the notes point to it.

## Connecting a Volvo

Each self-hosted instance uses its own application, created and published on the [Volvo Cars developer portal](https://developer.volvocars.com/). Its settings cannot be changed after publication, so choose the redirect URI first: it must lead to `/auth/volvo/callback` on the host where you sign in, in https (or http on `localhost`/`127.0.0.1`, if Volvo accepts it: not verified yet). In a self-hosted instance, that is `runsten-web`, which relays it to `runsten-api` ([Self-hosting](#self-hosting)); with `runsten-api` alone, as below, `runsten-api` itself.

```sh
RUNSTEN_VOLVO_CLIENT_ID=… RUNSTEN_VOLVO_CLIENT_SECRET=… RUNSTEN_VOLVO_API_KEY=… \
RUNSTEN_VOLVO_REDIRECT_URI=https://runsten.example.org/auth/volvo/callback \
RUNSTEN_DATABASE_URL=… RUNSTEN_TOKEN_KEY=… runsten-api
```

Sign in ([Users](#users)), then open the start URL that `runsten-api` logs (`…/auth/volvo/start`, also linked from the home page while there is no vehicle), in a browser, on the host of the redirect URI, and log in with your Volvo ID. The connection and its vehicles are recorded in your account. The tokens are stored encrypted. `runsten-collector` (with the same client ID and secret) refreshes them before they expire and at least once a day. A Volvo grant lasts 6 months at most: when it ends, or if Volvo refuses a refresh, the collector stops polling, logs `re-authentication required` and shows it on its debug page; the web interface says so on every page of the vehicle, with a "Reconnect the Volvo ID" button. Open the start URL again.

The web interface's **Connection** page tells the rest: when the Volvo ID was authorized and renewed, the application key the vehicles are read with, and for each vehicle what the collector wrote after its latest pass (its polling mode, its latest successful reading and the next one due, an exhausted quota, a pause, its latest failed call). The pages of a vehicle warn when nothing new is read of it: the grant lost, a key refused or missing, a pause, a quota, or a collector that has not passed for 15 minutes.

**Application key.** `RUNSTEN_VOLVO_API_KEY` is the key (`vcc-api-key`) of your application: it reads every vehicle of the instance, and Volvo counts the quota on it. The Connection page shows which application the instance uses, by its client ID and the last four characters of its key, to check them against the developer portal; the secret is never shown. Left empty, the instance has no key of its own, as the hosted offer: each account then gives, on the Connection page, before connecting its Volvo ID, the key of an application it creates on the developer portal and leaves unpublished (no redirect URI, no terms, no review); the Volvo ID still connects through the instance's application, whose client ID and secret refresh the tokens. Volvo counts the quota on the application of the key, whichever application issued the token (tried on the real API), so each account spends its own; a key never reads another account's vehicles. With `RUNSTEN_VOLVO_API_KEY` set, an account may not give a key. An account's key is stored encrypted like the tokens, bound to its account, never shown again (only its last four characters and when it was given) and never logged. With a Volvo ID connected, a new key is checked by listing the vehicles with it: a key Volvo refuses is not stored. If Volvo later refuses a key (regenerated, or its application deleted on the portal), its vehicles are no longer read, the Connection page says "Your Volvo key was refused", and the grant is left alone: the collector keeps the tokens alive, and a corrected key reads again without connecting the Volvo ID again. The instance's key refused is tried again every hour (logged as `application key refused`). Assumption: Volvo's gateway refuses a key with a 401 or 403 whose message names the key ("invalid subscription key"); a refusal worded otherwise is taken for the token's.

`runsten-api` serves plain http: keep it on `127.0.0.1` (the default), and reach it from elsewhere through an https reverse proxy ([Reverse proxy](#reverse-proxy-https)), otherwise passwords and session cookies travel in clear text. It warns when it listens on another address, unless `RUNSTEN_ACCESS_RESTRICTED=true` says that the deployment only lets it be reached through the host's loopback or such a proxy, as `compose.yaml` does.

**Quota.** Volvo grants 10,000 calls a day per API (Connected Vehicle, Energy, Location) and per application key: the developer portal counts them on the application of the key a call is made with. The collector keeps a budget per key and per API, and spreads 90 % of it over the day, up to an hour's worth at once. A vehicle uses about 550 calls a day parked, 385 of them on Connected Vehicle, the API that runs out first (some 2,200 driving ten hours, 1,500 on Connected Vehicle: engine state, charging state and odometer every minute while it drives), so on one key the budget only bites beyond some twenty vehicles parked, six driving ten hours a day; past it, calls wait (logged as `quota budget spent`), the accounts that called the least lately first. With the instance's key, every vehicle of the instance counts on it; without one, each account's key is its own, and an exhausted quota (Volvo's 403) blocks that key only. The calls are counted in the database, so a restart does not spend the day's budget again. If Volvo raised your quota, set `RUNSTEN_VOLVO_DAILY_QUOTA` to it; if it counts the instance key's per user (not documented), set `RUNSTEN_VOLVO_QUOTA_PER_USER=true`: each account then has its own.

The application needs the read scopes of the polled endpoints, which `runsten-api` requests by default: `openid`, `conve:vehicle_relation`, `conve:engine_status`, `conve:odometer_status`, `conve:trip_statistics`, `conve:diagnostics_workshop`, `conve:brake_status`, `conve:diagnostics_engine_status`, `conve:fuel_status`, `conve:tyre_status`, `conve:warnings`, `conve:doors_status`, `conve:lock_status`, `conve:windows_status`, `energy:state:read`, `energy:capability:read` and `location:read`. Tick them on the developer portal. `location:read` reportedly requires a review by Volvo; without it, or another, set `RUNSTEN_VOLVO_SCOPES` to the list your application has. The brakes, engine and fuel endpoints (`conve:brake_status`, `conve:diagnostics_engine_status`, `conve:fuel_status`) are optional: a vehicle whose token lacks their scope, or whose model lacks them, is read without them, and they are tried again a day later (logged once as `optional endpoint unavailable`). A Volvo ID connected before a scope was added grants it only once connected again.

## Self-hosting

Runsten runs with Docker Compose on any Linux server with Docker Engine 25 or later: a NAS, a VPS, a Raspberry Pi 4 or 5 on a 64-bit system. The images are built for `linux/amd64` and `linux/arm64`. The stack is PostgreSQL 18, `runsten-api` (sign-in, the Volvo ID connection and the API), `runsten-collector` and `runsten-web` (the web interface, which relays to `runsten-api` at the same origin). Its data lives in the `runsten_postgres` volume, which survives restarts and updates; the containers restart on their own (`unless-stopped`).

### Installation

```sh
git clone --branch v<X.Y.Z> <repository> runsten && cd runsten   # the latest release
cp .env.example .env && chmod 600 .env   # fill it in, following its comments
docker compose pull && docker compose up -d
docker compose ps                         # postgres, api, collector and web: running (healthy)
docker compose run --rm api user create <name>   # your user: asks for a password, twice
docker compose logs -f api collector
```

`.env` holds the passwords, the token encryption key and your Volvo application ([Connecting a Volvo](#connecting-a-volvo)). Generate the secrets with standard tools:

```sh
openssl rand -hex 24      # POSTGRES_PASSWORD, RUNSTEN_DB_PASSWORD
openssl rand -base64 32   # RUNSTEN_TOKEN_KEY
```

**Keep a copy of `RUNSTEN_TOKEN_KEY`** in a password manager, apart from the backups. It encrypts the Volvo tokens stored in the database and is stored nowhere else: without it, the tokens cannot be decrypted and the Volvo ID must be connected again. The collected data does not depend on it.

A configuration error stops `api`, `collector` or `web` at startup, with the reason in `docker compose logs`. Compose itself refuses to start without the required variables.

What the stack exposes:

- the web interface (`runsten-web`) on `127.0.0.1:8082` of the host only (`RUNSTEN_WEB_PORT` to change the port): the one to open, and to put behind the reverse proxy;
- `runsten-api` alone on `127.0.0.1:8081` only (`RUNSTEN_API_PORT`): its own minimal pages (sign-in, Volvo ID connection) and the API, for scripts, and a fallback should `web` be down;
- the collector's debug page, when enabled, on `127.0.0.1:8090` only;
- with the `mqtt` profile, a Mosquitto broker for trying the MQTT publishing on `127.0.0.1:1883` only (`RUNSTEN_MQTT_PORT`);
- PostgreSQL on no port: only `api` and `collector` reach it, on an internal network (`runsten-web` has neither database nor secret). Runsten connects as `runsten`, the owner of the database, which is not a superuser.

Health: `docker compose ps` shows each service's health. The images have no shell, so the healthcheck runs the binary itself (`/app healthcheck`), which requests `/healthz`: the API and the web interface answer while they serve, the collector while its passes go through (it serves `/healthz` on the loopback of its container only). Docker does not restart an unhealthy container; it restarts one that stops.

### Users

An instance has one user, created from the command line: there is no default account, and the password never goes through an argument, a variable or the logs.

```sh
docker compose run --rm api user create <name>     # asks for the password twice, without echo
docker compose run --rm api user password <name>   # a new password; signs out every session
printf '%s\n' "$password" | docker compose run --rm -T api user create <name>   # from a script
```

Passwords have at least 15 characters (a passphrase or a password manager), and are stored hashed with argon2id. Signing in (the web interface, or `runsten-api`'s own `/login`) opens a session for 30 days, ended sooner by 7 days without use or by signing out. After 10 failed attempts within 15 minutes, for a username or from an address, attempts are refused until the 15 minutes are over; restarting `runsten-api` clears this. A forgotten password is replaced with `user password`.

The settings end with **Your data**: **Download my data** saves everything the account holds as one JSON file: every table of the account, the raw readings included, read from one snapshot of the database; the tokens, the application key and the password hash are left out. To start afresh, start a new instance.

### Connecting your Volvo ID

Open the web interface, sign in, then follow "Connect a Volvo ID" on the home page (the start URL that `runsten-api` logs), and log in with your Volvo ID:

- behind a reverse proxy (below): `https://runsten.example.org/`;
- without one, on the server itself or through an SSH tunnel (`ssh -L 8082:127.0.0.1:8082 server`): `http://127.0.0.1:8082/`. The redirect URI is then `http://127.0.0.1:8082/auth/volvo/callback`, if Volvo accepts it for a published application (not verified yet).

The redirect URI registered with your Volvo application must be the one in `.env`, on the host your browser uses: it cannot be changed after publication, so settle the public host name first.

### Reverse proxy (https)

Runsten authenticates its users, but serves plain http: the reverse proxy brings https, so that passwords and session cookies never travel in clear text. Put it in front of `runsten-web`, which serves the interface and relays `/api/` and `/auth/` to `runsten-api`. An example with [Caddy](https://caddyserver.com/) on the host, which obtains and renews the certificate itself (the name must resolve to the server, ports 80 and 443 open):

```caddyfile
runsten.example.org {
	reverse_proxy 127.0.0.1:8082
}
```

Then, in `.env`, `RUNSTEN_VOLVO_REDIRECT_URI=https://runsten.example.org/auth/volvo/callback` (the application registered with the same URI), and `docker compose up -d`. The https redirect URI makes the session cookie `Secure`. If Caddy runs in a container, attach it to the `runsten_default` network and proxy to `web:8082`. Never publish the debug page through the proxy: it shows VINs and locations; `runsten-api`'s own port needs no publishing either.

Under a path rather than a host of its own, the proxy strips the prefix and `RUNSTEN_WEB_BASE_PATH` tells it to the interface, which writes it into its `<base href>`; the redirect URI includes it:

```caddyfile
example.org {
	handle_path /runsten/* {
		reverse_proxy 127.0.0.1:8082
	}
}
```

with `RUNSTEN_WEB_BASE_PATH=/runsten/` and `RUNSTEN_VOLVO_REDIRECT_URI=https://example.org/runsten/auth/volvo/callback`.

The proxy must keep the `Host` header (Caddy and most proxies do), and so does `runsten-web` when it relays: `runsten-api` refuses the cross-origin requests that change state by comparing `Origin` with it. Behind the proxy, failed sign-ins are counted per username and for the proxy's address as a whole, not per visitor: someone who fails 10 times delays everyone's next sign-in by up to 15 minutes, but open sessions keep working. A `basic_auth` or an address filter at the proxy is no longer needed; it remains possible as an extra layer, at the price of two logins.

### MQTT and Home Assistant

Each account may set its MQTT broker in the settings: the collector publishes the vehicles' state there after each reading, and Home Assistant creates their entities by discovery. The topics, the entities and how to expose a broker are in the documentation (`documentation/guide/mqtt.md`). By default Runsten publishes to brokers on its own network; `RUNSTEN_MQTT_PRIVATE_BROKERS=false` restricts it to brokers on the Internet over TLS, for an instance whose users are not all trusted. To try it without a broker, the `mqtt` profile adds Mosquitto: `docker compose --profile mqtt up -d`, then `mqtt://mosquitto:1883` in the settings, and `docker compose exec mosquitto mosquitto_sub -v -t '#'`.

### Debug page

The Connection page of the web interface is meant for users; the debug page is for developers: every call with its raw response, VINs and locations included. Set `RUNSTEN_DEBUG_ADDR=0.0.0.0:8090` in `.env` (the container side), run `docker compose up -d`, and open `http://127.0.0.1:8090` on the server, or through `ssh -L 8090:127.0.0.1:8090 server`.

### Backup and restore

A backup is a dump of the database, taken by the PostgreSQL container while everything runs:

```sh
docker compose exec -T postgres pg_dump -U postgres --format=custom runsten > runsten-$(date +%F).dump
```

Schedule it with the host's cron or the NAS scheduler, and copy the dumps off the server. They contain VINs and locations: protect them. The tokens in them are encrypted with `RUNSTEN_TOKEN_KEY`: a dump restored without the key keeps all the data but loses the Volvo connection, which is reconnected from the start URL.

To restore, on the same server or a new one (with the same `.env`, key included):

```sh
docker compose up -d postgres            # on a new server, initializes an empty volume
docker compose stop api collector
docker compose exec -T postgres pg_restore -U postgres --dbname=runsten \
  --clean --if-exists --single-transaction --exit-on-error < runsten-2026-09-25.dump
docker compose up -d
```

Trips and charges are derived from the stored responses. To recompute them (after a restore, or a Runsten update that improves the derivation; the costs entered for charges are not derived, and are kept):

```sh
docker compose stop collector
docker compose run --rm collector rebuild
docker compose start collector
```

`task compose-test` plays this procedure against the simulator.

### Updating

Read the release's notes ([releases](https://github.com/runsten-app/runsten/releases)), back up, then set `RUNSTEN_VERSION` in `.env` to the new release and:

```sh
git fetch --tags && git checkout v<X.Y.Z>   # compose.yaml and .env.example of that release
docker compose pull && docker compose up -d
```

An update that changes how trips and charges are derived says so below: the new events follow it at once, the past ones only after a rebuild ([Backup and restore](#backup-and-restore): stop the collector, `docker compose run --rm collector rebuild`, start it).

- **Energies on the net capacity** (migration `0007`): the energy of a trip or a charge, ΔSoC × capacity, rests on the net (usable) capacity of the vehicle's variant when the variant is recognized or chosen and the catalog knows that capacity ([Vehicle models](#vehicle-models)), rather than on the one Volvo reports, which is the gross one: for an EX30 of 69 kWh gross, 64 net, energies, consumptions and costs are 7 % lower. Each trip and charge tells the capacity it rests on (`capacity_kwh`, and `capacity_source`: `catalog_net` or `api`). Rebuild after updating: until then, the past events keep their energies, without a capacity.
- **MQTT** (migrations `0016` and `0017`): an account may set its MQTT broker in the settings, to which the collector publishes the vehicles' state and Home Assistant's discovery ([MQTT and Home Assistant](#mqtt-and-home-assistant)). Nothing is published until one is set. No rebuild.
- **The account's data**: the settings can download all the account's data ([Users](#users)). No migration, no rebuild.
- **Application keys** (migration `0011`): `RUNSTEN_VOLVO_API_KEY` becomes optional; without it, each account gives its own key on the Connection page ([Connecting a Volvo](#connecting-a-volvo)). Nothing changes for an instance that keeps its key: every vehicle still reads with it, and the Connection page shows its client ID and the end of its key. No rebuild.
- **Spans and odometers of charges** (migration `0009`): each charge stores what the estimate of its battery capacity rests on: the stretch of its power integration where the energy and the SoC change were read at the same readings (the SoC at both ends, the integrated energy, the widest gap between two of its readings, and the time between its first and last one), kept up to 95 % SoC only — past it, the power goes into balancing the cells without raising the SoC — and the odometer at the reading that ended the charge. Nothing visible changes with this update: the battery page that reads them comes later. Rebuild after updating: until then, the past charges have no span and no odometer.

The new images replace the containers; the migrations are applied at startup, under a lock, whichever binary starts first. The PostgreSQL image is pinned by digest in `compose.yaml` and updated with Runsten. A new PostgreSQL major version (after 18) changes the format of the volume: Runsten will announce it, and the migration will be a backup, a new volume and a restore.

Images: each release publishes `ghcr.io/runsten-app/runsten-api`, `runsten-collector`, `runsten-web` and `runsten-simulator`, for `linux/amd64` and `linux/arm64`, tagged with its version (`0.4.1`), its minor (`0.4`, which follows the fixes) and `latest`, each with its SBOM and provenance; the release's notes give their digests. `RUNSTEN_VERSION` (`.env`) chooses the tag, `latest` when empty. To run the sources instead (a branch, a change of your own), `docker compose up -d --build` builds them under the same names.

## Web interface

`runsten-web` serves the interface at the root of its host (or under `RUNSTEN_WEB_BASE_PATH`), for a phone as well as a desktop, in English, French and Swedish (the language chosen in its menu, else the browser's), light or dark as the system is. Signed in, it shows for each vehicle:

- **State** (Verdandi): state of charge, range, charging, odometer, last position; each value with when it was last checked and since when it holds, marked stale when it is old, and a warning when the Volvo ID must be connected again; below them, the state of charge of the last 7 days as the car gave it, over bands of its trips and charges;
- **Trips** and **Charges** (Urd): by day, newest first, within a period kept in the URL, and each event's details, under a line of totals for the period ("3 trips started in the period · 73 km · 18.8 kWh/100 km (estimated)"). Times are bounds ("06:55–07:01 → 07:39–07:40"), durations the range they allow, and a reconstructed event says it was never seen ("Happened between 12:00 and 13:00"). Unknown values say so. Above each list, a chart of the period's distance or energy, whose bars narrow it to their interval. A trip's consumption is set beside its month's average and that of the trips of its length. A charge's page draws its power and state of charge against time, when it was read often enough for a curve;
- **Stats**: the totals of a period (this month by default, a shortcut or any dates) and charts by day, week or month, in three views (driving, charging, costs): distance, energy charged by AC and DC, consumption, time driving and charging, the loss of charge while parked, and, with a currency, the cost of the charges and the cost per 100 km. Selecting a bar opens the trips or charges of its interval. Below them, how the period's trips spread by distance and what each band consumed, the state of charge its charges started and ended at, and the energy charged at each of your places, elsewhere by AC and DC, and without a position, with, given a currency, what it cost there. Each trip and charge counts once, in the interval it started in; energies, consumption and the loss of charge while parked are estimates from the state of charge, and the page says so;
- **Battery**: the remaining capacity of the battery, estimated from its charges (the integrated charging power, or the energy billed on a receipt, over the rise of the state of charge): a gauge against the model's data sheet, with its margin, and a chart against the date or the mileage with the monthly median and its middle half, next to the evolution over the months and the car's own range at 100 %. It is an estimate, never a measurement of health: a single point is approximate, and the trend over the months tells more than the level.

**Costs**, once a currency is chosen in the settings:

- a charge shows its place (or "Outside any place") and its cost: estimated from its place's tariff, with the billed energy and the efficiency assumed ("30 kWh billed (estimated), assuming an efficiency of 88%"), or entered. The cost is a range when the charge's times allow several prices ("€5.24 – €5.79", read "from €5.24 to €5.79" by a screen reader), one amount otherwise; an unknown one says so, and a cost of exactly 0 is "Free". A charge with a position outside every place offers **Create a place here**, at its position;
- **Enter the cost** records what was paid, as on the receipt (the amount, and optionally the billed energy and a note): it replaces the tariff's estimate until it is deleted, and survives a rebuild;
- the lists of charges show each known cost, and their line of totals the sum of the known costs, with the charges whose cost is unknown and those entered; the statistics have a tile for the cost of the charges and a chart of it by interval, the lowest cost stacked with its uncertainty up to the highest;
- an entered cost that no charge takes any more (the charges were detected again, and none or several match it) is listed in the settings under **Costs without a charge**, to attach to a charge among those around its time, or delete. The statistics tell it apart ("1 cost without a charge: €12.40, not in the total"): added to the total, it would count its charge twice.

**Settings** (the cog in the bar) are the account's, not a vehicle's:

- **Currency**: the one every price and cost is in, without conversion. Without it, no cost is computed. Once a price or an entered cost exists, it can no longer change.
- **Vehicles**: each vehicle with its VIN, and the model and model year it reports. Its variant, recognized automatically when only one of the catalog fits its details, can be chosen among those of its family (each with its capacities, AC and DC powers, and whether they come from the manufacturer or public sources); the choice outranks the recognition, and "Automatic recognition" goes back to it. When the variant offers a 22 kW onboard charger as an option, the charger can be stated too; unstated, the more powerful is assumed. A model the catalog does not have (hybrids included) has no variant to choose ([Vehicle models](#vehicle-models)).
- **Places and tariffs**: up to 100 places, each a circle (a position and a radius, 100 m by default) with a tariff; a charge takes the tariff of the nearest place that holds it. The position is typed (a point or a comma as the decimal separator), taken from a vehicle's current position, or pressed on the map where the instance has one. A place may also take the AC charges without a position (at most one place), and may say its charger's power, which narrows the cost of a charge across several prices, and its charging efficiency (by default 0.88 in AC, 0.95 in DC). A tariff is a list of prices, each from a day on in the place's time zone ("New price from…" copies the latest): a base price, and time windows with their own price, by day of the week, hours and months. A window may cross midnight (22:00–06:00 on Monday is the night from Monday to Tuesday); where windows overlap, the last one wins. A new price leaves the past charges at theirs; correcting a price changes them. A tariff applies from its first day on, never before: when charges at the place started earlier (a new place's tariff starts today, after the charges that made you create it), its page says how many, and offers to start the tariff on the day of the first. Costs are computed on each reading, so a price moved earlier applies to the past charges as soon as it is saved, and a charge without a price says why.
- **Your data**: download everything the account holds, as one JSON file ([Users](#users)).

The forms check what the API would refuse before sending it; its errors are told under each field and in a summary at its top, each line leading to its field.

Positions are named by the place that holds them, else, where the instance has a reverse geocoder (`RUNSTEN_GEOCODER_URL`, off by default, a Nominatim or a provider speaking its API), by their street and town, which it is sent, and given as coordinates, with a link to OpenStreetMap, opened only on a click, where no map shows them. Maps (a trip's start and end, the vehicle's position, a place's circle) are off by default: `RUNSTEN_WEB_MAP_TILES` turns them on with a tile provider's URL (`https://tile.openstreetmap.org/{z}/{x}/{y}.png` suits a personal instance), or with the URL of a [PMTiles](https://docs.protomaps.com/pmtiles/) file of vector tiles, ending in `.pmtiles`, on storage of your own that answers range requests with CORS (an extract made with `pmtiles extract` from a [Protomaps build](https://maps.protomaps.com/builds/), drawn in the theme's light or dark colors), and `RUNSTEN_WEB_MAP_ATTRIBUTION` gives the attribution it requires (default © OpenStreetMap contributors; Protomaps © OpenStreetMap for a Protomaps build). The browser then fetches the tiles from that host, which learns its address and the areas shown; each reader can hide the maps in the preferences. The interface is a static build served with a strict Content Security Policy; it holds no secret, and the browser keeps only the session cookie and the chosen language. Its accessibility is checked in `task compose-test`: axe finds no WCAG 2.1 A or AA violation on any view, on a phone and a desktop, light and dark, and signing in and opening a trip work with the keyboard alone. Each chart is described in a sentence, and its figures are in a table for screen readers; its colors read at 3:1 at least. The same tests fail on any violation of the Content Security Policy.

## Vehicle models

Runsten carries a catalog of the electric Volvo and Polestar variants, [`internal/catalog/variants.yaml`](internal/catalog/variants.yaml): for each, its family and model years, its gross and net battery capacities, its onboard charger (standard, and the optional 22 kW one), its DC power, and the sources of every figure. It is built into the binaries: an update of Runsten brings its corrections, with no migration.

**Recognition.** The variant is worked out on each reading, from the details the Volvo API reports (read once a day) and the VIN; nothing more is collected:

1. the variants of the vehicle's family (`descriptions.model`: `EX30`, `XC40`) whose model years hold its model year;
2. among them, those already seen reporting its `batteryCapacityKWH` (for a few months the backend returned 66.0 for EX30s of 69 kWh), or else those whose gross capacity lies within 1.5 kWh of it;
3. the motor code of the VIN (positions 4–5, from unofficial tables) breaks the ties it can.

The vehicle is recognized when exactly one variant remains, and only a fully electric one (`fuelType` `ELECTRIC`, or `NONE` as an EX30 reports): a plug-in hybrid never is, since Runsten does not derive its charges correctly. When several variants remain, none, or the family is not in the catalog, the vehicle has no variant until one is chosen: its energies rest on the capacity Volvo reports, and its AC charges are bounded by the place's charger alone. Polestar variants are in the catalog, but no data source reports a Polestar yet.

**Choosing.** Settings → Vehicles lists the variants of the vehicle's family, the candidates of the recognition first. A chosen variant outranks the recognition as long as the vehicle reports the same family (a choice that no longer holds is ignored, and the recognition applies again); "Automatic recognition" goes back to it.

**Onboard charger.** It bounds the power of an AC charge whatever the charger of the place, which narrows its cost across several prices. When the variant offers a 22 kW charger as an option and none is stated, 22 kW is assumed: the higher bound never narrows a cost by mistake. State 11 kW if the car has the standard one.

**Net capacity.** An energy is ΔSoC × capacity. Volvo reports the gross capacity; Runsten assumes, until a real measurement confirms it, that the state of charge runs over the net (usable) one, and uses the variant's net capacity when the catalog knows it (`capacity_source: "catalog_net"`), else the reported one (`"api"`). Each trip and charge tells which, with the capacity itself (`capacity_kwh`).

To see it in development, the `ex30` scenario of the simulator runs an EX30 the catalog recognizes (`tilt up -- --scenario=ex30`, [Development](#development)).

**What a choice changes.** A charger applies at once: costs are computed on each reading. A new variant, or going back to the recognition, changes the energies, and with them consumptions and costs: the collector derives the vehicle's trips and charges again once it next stores a response of the vehicle (within 10 minutes while parked, unless the quota is exhausted), keeping the entered costs as a rebuild does. A vehicle whose Volvo ID must be connected again is not polled, so it waits for the reconnection, or a rebuild.

### Contributing a variant

A missing or wrong variant is a pull request on [`internal/catalog/variants.yaml`](internal/catalog/variants.yaml), whose header documents every field:

- one entry per variant, a powertrain and battery over a range of model years: `id` (lowercase words joined by hyphens, as `ex30-er-2024`), `brand`, `family` exactly as `descriptions.model` returns it, `name`, `years` (`[from, to]`, or `[from]` while on sale), `gross_kwh`, `net_kwh` when a source settles it, `chemistry`, `ac_max_kw`, `ac_option_kw`, `dc_max_kw`, `motor_codes`;
- `sources`: each an https URL, its `level`, `manufacturer` or `secondary` (press, Wikipedia; ev-database as a cross-check only, never copied), and `for`, the figures it backs. Every figure needs one, and a source backs only figures the entry has. The interface tells "from the manufacturer" from "from public sources" by these levels. Where sources disagree, a maximum power keeps the highest value, a capacity the manufacturer's, and a comment says so;
- `api_kwh`: the values of `batteryCapacityKWH` seen reported for the variant, each with a link to where (an issue, a forum post): a value that differs from the gross capacity is recognized only through them;
- `ambiguous_with`: two variants that no reading tells apart (same family, overlapping years, close capacities, no motor code to separate them) must name each other, and the user chooses between them; a mark that no longer matches an ambiguity is an error;
- never remove an entry nor change its `id`: users' choices refer to it, and a choice whose variant has gone is ignored. Correct its figures instead;
- no real VIN, anywhere: motor codes are two characters, and the tests build made-up VINs.

`go test ./internal/catalog/...` validates the file, as `runsten-api` and `runsten-collector` do at startup, which refuse to start on an error: required fields and their bounds, sources, and that every two variants can be told apart or are marked ambiguous. Tests that list variants (`TestEmbeddedCatalog`, `TestAmbiguousPairs`) change in the same pull request, and a case in `TestMatch` shows the reading that recognizes a new entry. A corrected net capacity, or a vehicle now recognized, changes the energies of the events derived after the update, and of the past ones after a rebuild.

## API

`runsten-api` serves a JSON API under `/api/v1`, for the web interface and for scripts: it reads the vehicles and their events, and writes the account's settings and the costs of charges. Every route requires a session: sign in with `POST /api/v1/session` (`{"username": …, "password": …}`), which sets the session cookie, and send the cookie back.

A program reads it with a personal access token instead, without the password: create one in the web interface (Settings, API access), or with `POST /api/v1/tokens` (`{"name", "expiry": "30d" | "90d" | "365d" | "never"}`), and send it in an `Authorization` header. The token is shown once; only its hash is kept. It only reads: it serves every `GET` route but those of the session and of the tokens, and anything else answers `403 insufficient_scope`. A token makes at most 120 requests at once, then 2 per second (`429 rate_limited`, with `Retry-After`). Revoke it in the settings, or with `DELETE /api/v1/tokens/{id}`: it stops at once. A new password leaves the tokens as they are.

```sh
curl -H "Authorization: Bearer rst_…" https://runsten.example/api/v1/vehicles
```

| Route | |
|---|---|
| `GET`, `POST`, `DELETE /api/v1/session` | the current session; sign in; sign out |
| `GET`, `POST /api/v1/tokens`, `DELETE …/tokens/{id}` | the user's access tokens, without their secrets; issue one (its secret in this answer only); revoke one. With the session only |
| `GET /api/v1/vehicles`, `…/vehicles/{id}` | the account's vehicles, their model (family, model year, and the variant in effect, chosen by the user or else recognized in the catalog, with its capacities and charging powers, and the onboard charger the user stated) the state of their Volvo connection (`active`, `reauth_required`), and what the collector wrote after its latest pass over each (`collection`) |
| `GET /api/v1/vehicles/{id}/variants` | the catalog's variants of the vehicle's family, the candidates of the recognition first, with the model years and where each figure comes from (manufacturer or public sources) |
| `PUT /api/v1/vehicles/{id}/model` | choose the variant and the onboard charger (`{"variant_id", "ac_max_kw"}`, `null` for the recognition and for a charger not stated); the choice outranks the recognition; the charger bounds the costs at once, and a new variant has the collector derive the vehicle's trips and charges again, on its net capacity |
| `GET /api/v1/vehicles/{id}/state` | the latest known values: SoC, range, odometer, position, charging; each with when it was read |
| `GET /api/v1/vehicles/{id}/trips`, `…/charges` | newest first, by pages (`limit`, `cursor`), within a period (`from`, `to`); each charge with its place and its cost |
| `GET /api/v1/vehicles/{id}/trips/{trip}`, `…/charges/{charge}` | one trip or charge |
| `GET /api/v1/vehicles/{id}/trips.csv`, `…/charges.csv` | every trip or charge of a period (`from`, `to`, as the lists), as a CSV file for a spreadsheet: oldest first, the fields of the lists flattened (`start_after`), an unknown value an empty cell, a charge's cost in major units (`cost_min`, `cost_max`) |
| `PUT`, `DELETE /api/v1/vehicles/{id}/charges/{charge}/cost` | enter what a charge cost (`{"amount_minor", "energy_kwh", "note"}`), in place of its tariff's; delete it |
| `GET /api/v1/vehicles/{id}/stats` | totals of a period (`from`, `to`), by `day`, `week` or `month` in a time zone (`bucket`, `tz`): distance, driving time, consumption, energy charged by type, cost of the charges, loss while parked |
| `GET /api/v1/vehicles/{id}/battery` | the estimated battery capacity over the vehicle's whole history: one estimate per charge, the current capacity and its deviation from the reference, the monthly medians in a time zone (`tz`) |
| `GET`, `PUT /api/v1/settings` | the account's currency, and the accepted ones; set it (it can no longer change once a tariff exists); with the limits the writes are checked against and the default charging efficiencies |
| `GET`, `POST /api/v1/places` | the account's places (home, work…): a circle, a time zone and a tariff in dated versions, with time windows; create one (at most 100) |
| `GET`, `PUT`, `DELETE /api/v1/places/{place}` | one place; replace it whole, tariff included; delete it |
| `GET /api/v1/places/{place}/unpriced` | the place's charges its tariff gives no price, as they started before its first version (`{"charges", "first_day"}`): a tariff from `first_day` prices them all |
| `GET /api/v1/charge-costs/orphans` | the entered costs that no charge takes any more, after a rebuild changed the charges |
| `PUT /api/v1/charge-costs/orphans/{cost}/charge`, `DELETE …/orphans/{cost}` | attach one to a charge (`{"vehicle", "charge"}`); delete it |
| `GET /api/v1/account/export` | all the account's data, as one JSON document (`{"format": "runsten-account", "schema", "tables": [{"name", "rows"}]}`), secrets left out, as a download |

Units are in the field names (`_km`, `_m`, `_kwh`, `_pct`, `_w`, `_kw`, `_s`, `_minor` in minor units of the account's currency, as cents, `price_per_kwh` in its major units), times are RFC 3339 in UTC, and an unknown value is `null`, never zero. Trip and charge times are bounds (`start.after`, `start.before`, `end.after`, `end.before`), and `reconstructed` marks the events that were never seen. Energies are estimates, ΔSoC × `capacity_kwh`: the net capacity of the vehicle's variant in the catalog (`capacity_source: "catalog_net"`, assuming the state of charge runs over the usable capacity), or else the capacity Volvo reports (`"api"`). Errors are `{"error": {"code": …, "message": …}}`. A request body must be sent as `application/json`.

The statistics count each trip and charge once, in the interval that holds its `start.after` (the lists, instead, keep every event that may overlap the period). A sum covers the known values and its `_unknown` companion counts the events that lacked one; an average over nothing is `null`. Durations are `{"min", "max"}` in seconds: every total the bounds allow lies within. At most 400 intervals; `tz` is an IANA time zone, `UTC` by default.

The battery reads the whole history of the vehicle, not a chosen period: one capacity estimate per charge — the energy it took over the SoC change of the same readings, from the integral of the charging power or from the energy billed on an entered receipt, in kWh — and their aggregates. The current capacity is the median of the latest 20 estimates; the deviation compares it with the reference capacity (the variant's net capacity, else what Volvo reports), signed and never clamped, so that a bias of the method shows as one; the change waits for 12 months of history and 40 estimates, and the equivalent full cycles are an estimate too. None of it is a measurement of health: the vehicle exposes no energy meter and the SoC is an integer, `excluded` counts the charges each filter set aside, and `range_at_full` is the vehicle's own forecast of its consumption (it follows the season and the driving), never a measure of the battery. The months, empty ones included, are split in `tz`; at most 400 of them.

A charge has a `place` (the nearest place whose circle holds its position) and a `cost`: `{"currency", "min_minor", "max_minor", "source", "energy_kwh", "efficiency", "note"}`, or `null` when unknown (no currency, outside every place without an entered cost, no energy estimate, or no tariff version for the charge's time). A tariff's cost is an estimate: the energy drawn from the grid is `energy_soc_kwh` over an assumed efficiency (0.88 AC, 0.95 DC, or the place's), and it is an interval, since the charge's times are bounds and the price may change within them; `min_minor = max_minor` when it does not. The power bounds how much energy each price can take, and narrows the interval: the place's charger, and for an AC charge the vehicle's onboard charger, from its variant: the one the user stated, else the optional 22 kW one when the variant has it (nothing says the car lacks it). An entered cost (`source: "entered"`) replaces it, survives a rebuild, and is found again by the charge's detection time or window; when it cannot be, it is an orphan, never lost, to attach or delete. The statistics sum the known costs (`cost`, `cost_unknown`, `cost_entered`) and show the orphans apart (`orphaned_costs`): adding them would count a charge twice.

The API is described by an OpenAPI 3.1 document, [`api/openapi.yaml`](api/openapi.yaml): routes, parameters, schemas, error codes, and what the fields mean (time bounds, reconstructed events, the two energy estimates). It is generated from the code of the API (built with [huma](https://huma.rocks)), and the tests check the actual responses against it, errors included, as well as the reference responses of [`internal/api/testdata/`](internal/api/testdata/). Do not edit it by hand: change the code, then run `go test ./internal/api -update`. Open it in any OpenAPI viewer, or render it with Redocly CLI:

```sh
docker run --rm --user "$(id -u):$(id -g)" -v "$PWD/api:/spec" -w /spec redocly/cli build-docs openapi.yaml -o api.html
# then open api/api.html (ignored by git)
```

Clients can generate their types from it, for instance with `npx openapi-typescript api/openapi.yaml -o api.ts`.

```sh
curl -c jar -H 'Content-Type: application/json' -d '{"username":"admin","password":"…"}' http://127.0.0.1:8081/api/v1/session
curl -b jar http://127.0.0.1:8081/api/v1/vehicles
```

## Licence

[AGPL-3.0-or-later](LICENSE).

---

Runsten is an independent project, not affiliated with Volvo Car Corporation or Polestar. "Volvo" and "Polestar" are trademarks of their respective owners.
