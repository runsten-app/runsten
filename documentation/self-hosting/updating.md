---
description: "Update a self-hosted Runsten, and the notes of the updates that change past trips and charges."
---

# Updating

Read the release's notes ([releases](https://github.com/runsten-app/runsten/releases)) and [back up](./backup.md) first. Then, in the directory of the [installation](./index.md#install), fetch the files of the new release (`.env` is left alone):

```sh
v=X.Y.Z   # the new release
for f in compose.yaml .env.example deploy/postgres/init-runsten.sh deploy/mosquitto/mosquitto.conf; do
  curl -fsSL --create-dirs -o "$f" "https://raw.githubusercontent.com/runsten-app/runsten/v$v/$f"
done
```

Compare `.env` with the new `.env.example` (a release that needs a new variable says so in its notes), set `RUNSTEN_VERSION=X.Y.Z` in `.env`, and:

```sh
docker compose pull && docker compose up -d
```

From a clone of the repository, `git fetch --tags && git checkout vX.Y.Z` brings the same files.

The new images replace the containers; the database migrations are applied at startup.

An update that changes how trips and charges are worked out says so below: the new events follow it at once, the past ones only after a [rebuild](./backup.md#rebuild).

## Update notes

- **Energies on the net capacity** (migration `0007`): the energy of a trip or a charge rests on the net (usable) capacity of the vehicle's variant when the catalog knows it, rather than on the gross one Volvo reports: for an EX30 of 69 kWh gross, 64 net, energies, consumptions and costs are 7 % lower. Rebuild after updating: until then, the past events keep their energies.
- **Spans and odometers of charges** (migration `0009`): each charge stores what the battery's estimate rests on. Nothing visible changes with it; the battery page reads it. Rebuild after updating: until then, the past charges give no estimate.

## PostgreSQL

The PostgreSQL image is pinned in `compose.yaml` and updated with Runsten. A new major version (after 18) changes the format of the volume: Runsten will announce it, and the migration will be a backup, a new volume and a restore.
