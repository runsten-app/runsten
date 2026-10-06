---
description: "Install Runsten with Docker Compose on a NAS, a VPS or a Raspberry Pi: the services, the secrets and what the stack exposes."
---

# Installation

Runsten runs with Docker Compose on any Linux server with Docker Engine 25 or later: a NAS, a VPS, a Raspberry Pi 4 or 5 on a 64-bit system. The images are built for `linux/amd64` and `linux/arm64`.

The stack has four services:

| Service | Role |
|---|---|
| `postgres` | PostgreSQL 18, the database |
| `api` | `runsten-api`: sign-in, the Volvo ID connection and the API |
| `collector` | `runsten-collector`: polls the Volvo API, works out trips and charges |
| `web` | `runsten-web`: the web interface, which relays to `api` at the same origin |

Its data lives in the `runsten_postgres` volume, which survives restarts and updates; the containers restart on their own.

## Before you start

Create your application on the Volvo developer portal first: its redirect URI cannot be changed afterwards, so settle the host name you will use ([Your Volvo application](./volvo.md)).

## Install

Nothing to build: the stack runs the published [images](#images). It needs only `compose.yaml`, `.env.example` and the two files of `deploy/` of a release, fetched into a directory of their own:

```sh
mkdir runsten && cd runsten
v=X.Y.Z   # the latest release: https://github.com/runsten-app/runsten/releases
for f in compose.yaml .env.example deploy/postgres/init-runsten.sh deploy/mosquitto/mosquitto.conf; do
  curl -fsSL --create-dirs -o "$f" "https://raw.githubusercontent.com/runsten-app/runsten/v$v/$f"
done
cp .env.example .env && chmod 600 .env   # fill it in, following its comments; RUNSTEN_VERSION=X.Y.Z
docker compose pull && docker compose up -d
docker compose ps                         # postgres, api, collector and web: running (healthy)
docker compose run --rm api user create <name>   # your user: asks for a password, twice
docker compose logs -f api collector
```

`.env` holds the passwords, the token encryption key and your Volvo application. Generate the secrets with standard tools:

```sh
openssl rand -hex 24      # POSTGRES_PASSWORD, RUNSTEN_DB_PASSWORD
openssl rand -base64 32   # RUNSTEN_TOKEN_KEY
```

::: danger Keep a copy of RUNSTEN_TOKEN_KEY
Put it in a password manager, apart from the backups. It encrypts the Volvo tokens stored in the database and is stored nowhere else: without it, the tokens cannot be decrypted and the Volvo ID must be connected again. The collected data does not depend on it.
:::

A configuration error stops `api`, `collector` or `web` at startup, with the reason in `docker compose logs`. Compose itself refuses to start without the required variables.

Then [connect your Volvo ID](./volvo.md#connecting-your-volvo-id).

## What the stack exposes

- the web interface on `127.0.0.1:8082` of the host only (`RUNSTEN_WEB_PORT` to change the port): the one to open, and to put behind a [reverse proxy](./reverse-proxy.md);
- `runsten-api` alone on `127.0.0.1:8081` only (`RUNSTEN_API_PORT`): its own minimal pages and the API, for scripts, and a fallback should `web` be down;
- the collector's debug page, when enabled, on `127.0.0.1:8090` only ([Troubleshooting](./troubleshooting.md#debug-page));
- PostgreSQL on no port: only `api` and `collector` reach it, on an internal network. Runsten connects as `runsten`, the owner of the database, which is not a superuser.

Everything listens on the host's loopback: from another machine, go through an https reverse proxy, or an SSH tunnel (`ssh -L 8082:127.0.0.1:8082 server`).

## Images

Each release publishes `ghcr.io/runsten-app/runsten-api`, `runsten-collector` and `runsten-web`, for `linux/amd64` and `linux/arm64`, tagged with its version (`0.4.1`), its minor (`0.4`, which follows the fixes) and `latest`, each with its SBOM and provenance; the release's notes give their digests. `RUNSTEN_VERSION` in `.env` chooses the tag, `latest` when empty: set it to a release, so that an update is a choice ([Updating](./updating.md)).

To run the sources instead (a branch, a change of your own), clone the repository and run `docker compose up -d --build`: it builds the images under the same names.
