---
description: "Update a self-hosted Runsten, and the notes of the updates that change past trips and charges."
---

# Updating

[Back up](./backup.md) first, then:

```sh
git pull
docker compose up -d --build
```

The new images replace the containers; the database migrations are applied at startup.

An update that changes how trips and charges are worked out says so below: the new events follow it at once, the past ones only after a [rebuild](./backup.md#rebuild).

## Update notes

- **Energies on the net capacity** (migration `0007`): the energy of a trip or a charge rests on the net (usable) capacity of the vehicle's variant when the catalog knows it, rather than on the gross one Volvo reports: for an EX30 of 69 kWh gross, 64 net, energies, consumptions and costs are 7 % lower. Rebuild after updating: until then, the past events keep their energies.
- **Spans and odometers of charges** (migration `0009`): each charge stores what the battery's estimate rests on. Nothing visible changes with it; the battery page reads it. Rebuild after updating: until then, the past charges give no estimate.

## PostgreSQL

The PostgreSQL image is pinned in `compose.yaml` and updated with Runsten. A new major version (after 18) changes the format of the volume: Runsten will announce it, and the migration will be a backup, a new volume and a restore.
