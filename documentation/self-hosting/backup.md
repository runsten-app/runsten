---
description: "Back up and restore the database of a self-hosted Runsten, and rebuild trips and charges from the stored readings."
---

# Backup and restore

## Backup

A backup is a dump of the database, taken by the PostgreSQL container while everything runs:

```sh
docker compose exec -T postgres pg_dump -U postgres --format=custom runsten > runsten-$(date +%F).dump
```

Schedule it with the host's cron or the NAS scheduler, and copy the dumps off the server. They contain VINs and locations: protect them.

The tokens in them are encrypted with `RUNSTEN_TOKEN_KEY`: a dump restored without the key keeps all the data but loses the Volvo connection, which is then connected again.

## Restore

On the same server or a new one, with the same `.env`, key included:

```sh
docker compose up -d postgres            # on a new server, initializes an empty volume
docker compose stop api collector
docker compose exec -T postgres pg_restore -U postgres --dbname=runsten \
  --clean --if-exists --single-transaction --exit-on-error < runsten-2026-09-25.dump
docker compose up -d
```

## Rebuild

Trips and charges are worked out from the stored responses of the car. To work them out again, after a restore or an update that improves how they are found:

```sh
docker compose stop collector
docker compose run --rm collector rebuild
docker compose start collector
```

The costs entered for charges are not worked out: a rebuild keeps them.
