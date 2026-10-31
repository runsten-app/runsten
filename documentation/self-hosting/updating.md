---
description: "Update a self-hosted Runsten, and the notes of the updates that change past trips and charges."
---

# Updating

## Knowing a release is out

**Settings → Account → About Runsten** shows the release the instance runs. With `RUNSTEN_UPDATE_CHECK=true` in `.env`, `runsten-api` reads the latest release on GitHub once a day, and that section tells a newer one, with its notes. It is off by default: nothing is asked of GitHub, and the section only links to the [releases](https://github.com/runsten-app/runsten/releases). Either way, `deploy/update.sh --check` tells it on demand, and the health report (`/healthz/full`, with `RUNSTEN_HEALTH_TOKEN`) gives the version running.

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
- **The vehicles' health**: a vehicle's page leads to its health (tyres, service, warnings), read from the responses already stored: a Volvo's history shows at once. MQTT publishes it under new topics (`tyre_pressure_…_kpa`, `tyre_warning`, `service…`, `warning_…`), and Home Assistant gets 15 new entities per vehicle; the existing topics are unchanged. A Polestar's needs its credential granted the Data Portal's optional health domain.
- **The place of a charge**: a charge takes the first position the car reports after it ends, rather than the one it had before. Unplugged at one charger and driven straight to another, a car may report its new position late: the second charge took the first one's place, its tariff and its cost. Rebuild after updating: the past charges whose car reported its position within 15 minutes of their end, before driving off or charging again, then take that position.
- **The place of a charge, again**: a charge after a drive Runsten did not see, revealed by the odometer read as the charge ended, now takes the position read just after it, and the next trip starts there. Rebuild after updating: until then, these charges and trips keep the previous place.
- **The place of a charge, for cars that stamp the odometer late**: a charge whose car recorded its position during the charge, read only after it ended, takes that position even when the odometer read in between reveals the drive to it; and a trip keeps its arrival when a position read later was recorded earlier. Rebuild after updating: until then, these charges and trips keep the previous place.
- **One charge while the cable stays connected**: a charge that stops with the cable connected (a solar charge, a car topping up later) and starts again within 24 hours, before it is unplugged or driven, is one charge rather than several. Rebuild after updating: until then, the past charges stay split.
- **The energy a Tesla measured** (migration `0026`): a Tesla's charge keeps the energy the car measured into its battery, and its cost rests on it rather than on the change of the state of charge. Rebuild after updating: until then, the past charges have none, and keep their cost.
- **A Tesla's software** (migration `0028`): a Tesla's page tells the version of its software and the update waiting for it, and its Health tab the versions it had, each with when it was installed. Rebuild after updating: until then, the versions read before are not listed.
- **The variants of the plug-in hybrids**: the catalog recognizes the plug-in hybrid Volvos (T6 and T8 of the 60 and 90 series, from model year 2016; T4 and T5 of the XC40) and the Polestar 1, and their energies rest on the net capacity of their variant when the catalog knows it (the 18.8 kWh pack, 14.9 net, and those of 10.4 and 10.7 kWh), rather than on the gross one Volvo reports: for a V60 of 2027, energies, consumptions and costs are 21 % lower. Their onboard charger bounds the costs of their AC charges. Rebuild after updating: until then, the past events keep their energies.
- **A Tesla's capacity from its rated range** (migration `0027`): a Tesla's charge keeps its rated range at its start and end, and its usable charge at the end, from which the battery page estimates the capacity as TeslaMate does. Rebuild after updating: until then, the past charges give no such estimate.
- **Trips and charges ended on the way**: a car read while it already drives off (a Tesla, read every few seconds) ended the charge or the trip before with that reading. A charge now keeps the position it charged at, and a trip the odometer it arrived with, rather than taking those of the drive that follows; and a trip that never moved, its odometer and its position unchanged (put in drive and back, to unplug), is no longer a trip. Rebuild after updating: until then, the past charges keep a position a few hundred metres off, which may leave them out of their place, and the past trips their odometers.
- **A Tesla's temperatures** (migration `0029`): a Tesla's page tells the outside and cabin temperatures and the climate, and each of its trips the average outside temperature of its readings, also in the trips' CSV file (a new column, `outside_temp_c`). Rebuild after updating: until then, the past trips have none.

## PostgreSQL

The PostgreSQL image is pinned in `compose.yaml` and updated with Runsten. A new major version (after 18) changes the format of the volume: Runsten will announce it, and the migration will be a backup, a new volume and a restore.
