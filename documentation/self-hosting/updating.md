---
description: "Update a self-hosted Runsten, and the notes of the updates that change past trips and charges."
---

# Updating

## With the update script

In the directory of the [installation](./index.md#install):

```sh
deploy/update.sh --check   # says whether a newer release is out
deploy/update.sh           # updates to the latest release
deploy/update.sh 0.11.0    # or to the one named
```

It finds the latest release on GitHub, shows the [update notes](#update-notes) since the release running and links to the release's notes, then asks before changing anything. It then pulls the new images, [backs the database up](./backup.md) into `backups/`, fetches the release's files (`.env` is left alone), sets `RUNSTEN_VERSION` in `.env` and restarts the stack, waiting until it is healthy. When a note asks for a [rebuild](./backup.md#rebuild), it offers one once the stack runs.

It stops, changing nothing, when:

- the release adds a variable to `.env.example` that `.env` lacks: it shows the variable with its comments; add it to `.env`, empty when you do not need it, and run it again;
- the images cannot be pulled, or the backup cannot be taken (`--no-backup` goes on without one: take one yourself).

`--yes` asks nothing, the rebuild included, for a scheduled update. The script needs bash, curl and Docker Compose. It runs the newer release's own `update.sh` when it differs, so that a release's new files and checks apply on the way to it. An installation from before the script has none: fetch it once, from the latest release (`X.Y.Z` below), which has it:

```sh
v=X.Y.Z   # the latest release: https://github.com/runsten-app/runsten/releases
curl -fsSL --create-dirs -o deploy/update.sh "https://raw.githubusercontent.com/runsten-app/runsten/v$v/deploy/update.sh"
chmod +x deploy/update.sh
```

The backups stay in `backups/`, one per update: copy them off the server with the others, and delete the old ones.

### Going back

Should a release not suit you, go back to the previous one with the backup the update took, named after that release:

```sh
deploy/update.sh --restore backups/runsten-0.10.1-20261026-103412.dump 0.10.1
```

The database is replaced with the backup, in a new database, as the migrations of the newer release are not undone otherwise: what was read since the update is lost. The script backs the database up before replacing it, and prints the command for the way back at the end of each update.

## By hand

Read the release's notes ([releases](https://github.com/runsten-app/runsten/releases)) and [back up](./backup.md) first. Then, in the directory of the installation, fetch the files of the new release (`.env` is left alone):

```sh
v=X.Y.Z   # the new release
for f in compose.yaml .env.example deploy/postgres/init-runsten.sh deploy/mosquitto/mosquitto.conf deploy/update.sh; do
  curl -fsSL --create-dirs -o "$f" "https://raw.githubusercontent.com/runsten-app/runsten/v$v/$f"
done
chmod +x deploy/update.sh
```

Compare `.env` with the new `.env.example` (a release that needs a new variable says so in its notes), set `RUNSTEN_VERSION=X.Y.Z` in `.env`, and:

```sh
docker compose pull && docker compose up -d
```

From a clone of the repository, `git fetch --tags && git checkout vX.Y.Z` brings the same files.

The new images replace the containers; the database migrations are applied at startup.

## Following latest

With `RUNSTEN_VERSION` empty or `latest`, `docker compose pull && docker compose up -d` alone brings the latest release, and with `0.10` the latest fix of that minor: no file is fetched, and no one chooses the update. A release that needs a new variable, a new file or a rebuild then starts without it, unannounced: prefer a release, which the script sets.

An update that changes how trips and charges are worked out says so below: the new events follow it at once, the past ones only after a [rebuild](./backup.md#rebuild).

## Update notes

<!-- deploy/update.sh shows the bullets added since the release running, one a line, and offers a rebuild when one says "Rebuild after updating". -->

- **Energies on the net capacity** (migration `0007`): the energy of a trip or a charge rests on the net (usable) capacity of the vehicle's variant when the catalog knows it, rather than on the gross one Volvo reports: for an EX30 of 69 kWh gross, 64 net, energies, consumptions and costs are 7 % lower. Rebuild after updating: until then, the past events keep their energies.
- **Spans and odometers of charges** (migration `0009`): each charge stores what the battery's estimate rests on. Nothing visible changes with it; the battery page reads it. Rebuild after updating: until then, the past charges give no estimate.
- **Net capacities of the Polestars**: the catalog gives the net capacity of every Polestar variant but the Polestar 2 Standard Range of 2022 and 2023 (its sources disagree), those of the Polestar 4 and 5 and of the Polestar 3 of 2026 included. The Data Portal reports no capacity: until then, the trips and charges of these variants had no energy. Rebuild after updating: until then, the past events stay without one.

- **The address, and Volvo optional**: `RUNSTEN_APP_URL` is the address Runsten is opened at, and the Volvo application is only needed to read a Volvo. An `.env` that sets `RUNSTEN_VOLVO_REDIRECT_URI` works as before; `RUNSTEN_APP_URL` may replace it, the redirect URI then being derived from it. An instance that reads only Polestars may leave the Volvo variables empty.

## PostgreSQL

The PostgreSQL image is pinned in `compose.yaml` and updated with Runsten. A new major version (after 18) changes the format of the volume: Runsten will announce it, and the migration will be a backup, a new volume and a restore.
