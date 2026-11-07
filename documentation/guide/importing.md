---
description: "Import your car's trips and charges from before Runsten read it: a Tesla's from a TeslaMate backup, a Volvo's from the files of the Volvo Data Portal (EU Data Act), any car's from Aximote, a car with Google built-in's from AAOS Logging."
---

# Importing your history

Runsten reads your car from the day you connect it. What came before may be in another app, or at the car's maker: Runsten imports it from these, by brand.

| Brand | Import | Gives |
|---|---|---|
| Tesla | [From TeslaMate](#from-teslamate), a backup of its database | drives with their tracks, charges |
| Volvo | [From the Volvo Data Portal](#from-the-volvo-data-portal) (EU), its files | trips with their tracks, no charges |
| Any brand | [From Aximote](#from-aximote), with a personal access token of its Pro offer | trips with their tracks, charges |
| Any brand with Google built-in | [From AAOS Logging](#from-aaos-logging), its export on Google Drive | trips with their tracks, no charges |

Every import is in **beta**: tell us if one of yours is refused, or if a trip or a charge looks wrong.

## How an import works

- **Where**: in the vehicle's settings (the menu of its name in the bar), under **Imported history**. Each import has its own part there, and its report shows in the same place.
- **Only what came before**: only what started before Runsten's first reading of the vehicle (or before the import, for a vehicle not read yet) is imported, so it never duplicates what Runsten saw. The report tells how many events were imported, how many were left because they start at or after that reading, and how many were left because they are unfinished or out of bounds.
- **Importing again** replaces what was imported, one by one: nothing is counted twice. **Deleting the import**, in the same place, removes every trip and charge it brought, with their tracks; what Runsten read stays.
- **Tracks**: a track is fine-grained location data. Runsten keeps it with the trip, reads it only to draw it on the trip's map, and deletes it with the import.

See [Trips and charges](./trips-and-charges.md) for how imported events show.

## Tesla

### From TeslaMate

A Tesla's years in [TeslaMate](https://github.com/teslamate-org/teslamate) come with it: Runsten reads the backup TeslaMate's documentation tells every user to make, without TeslaMate running nor its database being reachable. It has been tried on a backup of four and a half years, TeslaMate 1.20 to 4.4.

#### Making the backup

In the folder of TeslaMate's `docker-compose.yml`:

```sh
docker compose exec -T database pg_dump -U teslamate teslamate > teslamate.bck
```

Runsten reads that plain file, or the same gzipped (`teslamate.bck.gz`). A backup made with `pg_dump -Fc` (a custom-format archive) is refused: make it again without `-F`. It holds everything TeslaMate recorded, positions included: keep it as you keep TeslaMate's database, and delete the copy once imported.

#### Importing it

From the web interface: in the vehicle's **Imported history**, under **Import from TeslaMate**, press **Choose the backup**. Check **Import TeslaMate's costs** if they are in the account's currency: TeslaMate keeps none, and Runsten does not guess it. Then **Import**. The browser compresses the backup before sending it (a few years are about a gigabyte, several times less compressed): it still takes a few minutes on a slow connection, the page open.

On a self-hosted instance, the command line needs no upload at all, and the backup may even go straight from TeslaMate's database to Runsten:

```sh
docker compose exec -T database pg_dump -U teslamate teslamate \
  | docker compose -f ~/runsten/compose.yaml run --rm -T api import teslamate --vin YOUR_VIN --currency EUR
```

or from a file, in Runsten's folder: `docker compose run --rm -T api import teslamate --vin YOUR_VIN < teslamate.bck`. `--vin` is the vehicle's, as Runsten lists it; `--currency` imports the costs in that currency, left out without it; `--user` names the user whose account holds the vehicle, on an instance of several. The report is logged at the end.

Behind a reverse proxy, the web interface's upload must pass it: a large request body, and an answer that comes once the whole file is read ([Reverse proxy](../self-hosting/reverse-proxy.md#good-to-know): nothing to do with Caddy, two settings with nginx), or use the command line. A sender that stops for more than a minute is cut off.

#### What is imported

Only the car with this vehicle's VIN: a backup of several cars imports the one that is this vehicle, never another. Runsten reads the backup as it comes and keeps nothing of it but what follows.

- **Drives**: their exact times, distance, odometer, state of charge and rated range at both ends, the positions at both ends, the outside temperature, and their energy as TeslaMate counts it (the rated range used times the car's efficiency). No place names: Runsten names the positions as it names those it reads.
- **Tracks**: TeslaMate's positions of each drive, at most 500 per drive, evenly kept.
- **Charges**: their exact times, the position and the odometer, the state of charge, the energy the car counted into its battery, AC or DC (a fast charger, or AC phases), the highest current and the phases, the outside temperature, the rated range at both ends and the usable charge at the end, and the cost when asked for. With these, the [battery page](./battery.md#a-tesla) estimates the capacity at each imported charge as TeslaMate does: its years of capacity history come along.
- **Not imported**: TeslaMate's other readings (Runsten reads its own), its geofences and addresses (set your [places](./costs.md) in Runsten), and its software updates, yet.

A drive TeslaMate never closed, or a charge still in progress, is left out and counted as unfinished. Importing again replaces the drives and charges by TeslaMate's IDs.

## Volvo

### From the Volvo Data Portal

In the EU, the Data Act lets the user of a connected car ask its maker for the data it collects. Volvo Cars answers on its [Data Portal](https://data.volvocars.com/), which Runsten reads the files of. The import follows the files an owner published, and has not yet been tried on many.

1. On the Data Portal, sign in with your Volvo ID, choose the car, and ask for the **trips** and the **waypoints**, over the period you want. The portal takes several hours to prepare them.
2. Download the two CSV files it gives.
3. In the vehicle's **Imported history**, under **Import from the Volvo Data Portal**, press **Choose the files**: the trips file, required, and the waypoints file, optional. Then **Import**.

The import runs at once. Files of 32 MiB at most together: a year of waypoints is a few. A file without the columns of the portal's (the waypoints given as the trips, for one) or that is not a readable CSV is refused, and nothing is imported.

#### What is imported

- **Trips**: their exact times, the positions at both ends, the odometer at both ends and the distance (rounded to the kilometer by the portal), and the energy used, when the portal gives it (a trip of 0 kWh over a distance has none). No place names: Runsten names the positions as it names those it reads. No state of charge: the portal gives none.
- **Tracks**: the waypoints, about one a minute while driving, with a precise position. Those of a trip, from its start to its end, make its track.
- **No charges**: the portal gives none.

What the portal gives may be shorter than the period asked for: an owner found the last month missing. Fuel, regeneration and heading are given as 0 for an electric car: Runsten leaves them out. A trip that is unreadable or out of bounds (one that ends before it starts) is left out and counted in the report; a waypoint that is, is left out alone. Importing the same files again replaces the trips by their start time.

## Any brand

### From Aximote

Aximote, an app that logs the trips and charges of many brands, gives its history to Runsten. The import follows Aximote's documentation, and has not yet been tried on many real accounts.

1. In the Aximote app, create a personal access token (it starts with `axm_pat_`).
2. In the vehicle's **Imported history**, under **Add history**, paste it in **Aximote token**, then **Find my vehicles**.
3. Runsten lists the vehicles of your Aximote account. The one with this vehicle's VIN is chosen; when none has it, choose the one whose history this is: Runsten never guesses.
4. **Import**. The import goes on in the background: you can leave the page. Its progress, then its report, show in the same place.

Runsten keeps the token encrypted while the import runs and erases it as soon as it ends, whether it succeeded or not. It is never shown again, and never leaves Runsten for anything but Aximote. Revoking it in Aximote afterwards is harmless.

#### What is imported

- **Trips**: their exact times, distance, energy used and places, named as Aximote names them ("Lyon, France"). Neither state of charge nor odometer: Aximote gives none.
- **Charges**: the charging session's times, the energy charged, the average power, the cost Aximote gives, and the state of charge from the battery's energy at both ends, when Aximote gives it. Never AC or DC: Aximote does not tell, and Runsten does not guess it from the power.
- **Tracks** of the trips, when Aximote gives them: the path driven.

A trip still under way or a charge in progress is left out and counted as unfinished; fuel stops are left out without being counted. Importing again replaces what was imported, event by event.

#### When it fails

- **Aximote refused the token**: it was mistyped, revoked, or the Pro offer ended. Create another one.
- **Aximote kept failing**: try again later. When Aximote asks to wait, the import waits, then resumes where it stopped; a restart of Runsten resumes it too.

### From AAOS Logging

[AAOS Logging](https://play.google.com/store/apps/details?id=com.aaoslogging) is an app for cars with Google built-in (Android Automotive: Volvo, Polestar, Renault and others). It records each trip and the values the car gives the app (battery, range, temperatures…), and syncs them to your own Google Drive: no manufacturer account, in any country. Runsten reads that export. The import follows the format the app's developer documents ([aaos-drive](https://github.com/devmcage/aaos-drive), Drive sync V3, trip schema 17), and has not yet been tried on many real exports.

1. In AAOS Logging, turn on the Google Drive sync, and let it sync: the app writes to `AAOSLogging/<car>_<id>/Sync/V3` on your Drive.
2. In Google Drive, on the web, download the car's folder, `AAOSLogging/<car>_<id>`: Drive gives a ZIP file. Leave it as it is. Its `Sync` folder alone is enough, or its `Sync/V3` folder, or the `Commits` and `Artifacts` folders inside `V3` chosen together: the car's `Backup` folder is not read.
3. In the vehicle's **Imported history**, under **Import from AAOS Logging**, press **Choose the export**, choose the ZIP, then **Import**.

The import runs at once. A ZIP of 512 MiB at most. Runsten reads the newest generation the app synced, and checks every file of it against its commit, size and SHA-256: a folder downloaded while the app was still syncing, one with a file missing, or the whole `AAOSLogging` folder of several cars, is refused, and nothing is imported. Download it again once the app has synced, or the one car's folder.

The export names no VIN: it is imported into the vehicle you import it from. The part shows on every vehicle but a Tesla, which has no Google built-in.

#### What is imported

- **Trips**: their exact times, the positions at both ends, the app's distance, the state of charge and the range the car estimated at both ends (the first and last samples telling them), the mean of the car's outside temperature, and the energy used: the battery's net energy over the trip, when the app measured or derived it and it is positive. A plug-in hybrid's trip keeps neither the range nor the energy: its range counts the fuel too, and the battery's energy is not all the trip used. No odometer: the app's distance comes from the odometer, the GPS or the speed, and is no odometer reading. No place names: Runsten names the positions as it names those it reads.
- **Tracks**: the app's route of each trip, with its precise positions.
- **No charges**: the app records trips only.
- **Not imported, yet**: the app's other telemetry (tyre pressures, battery temperature, speed, power over the trip…) and its weather samples.

A trip the app has not closed is left out and counted as unfinished. Importing again replaces the trips by the app's trip IDs.
