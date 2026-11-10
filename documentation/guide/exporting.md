---
description: "Download your trips, charges and every value your car reported as CSV files, and all of your account's data."
---

# Exporting your data

Runsten keeps what your car's API answered, as it answered it, and gives it back to you: as CSV files, and as the archive of everything it holds.

## Trips and charges

Above the **Trips** and **Charges** lists, **Download CSV** saves the list's period as a CSV file for a spreadsheet: one row per trip or charge, the columns named after the API's fields (`start_after`, `energy_soc_kwh`, `cost_min`…). The charges' file follows the place chosen ([Charges by place](./trips-and-charges.md#charges-by-place)).

## Every reading

Beside it, **Download every reading (CSV)** saves every value the car reported over the list's period, including those no page of Runsten shows: the doors and windows of a Volvo, the charging current of a Polestar, the climate and configuration of a Tesla. One row per value:

| fetched_at | source | endpoint | field | value |
|---|---|---|---|---|
| 2026-10-14T07:01:12Z | volvo | doors | data.frontLeftDoor.value | CLOSED |
| 2026-10-14T07:01:12Z | volvo | doors | data.frontLeftDoor.timestamp | 2026-10-14T06:58:40.000Z |
| 2026-10-14T07:02:03Z | tesla | vehicle_data | response.charge_state.battery_level | 69 |
| 2026-10-14T07:02:03Z | tesla | vehicle_data | response.vehicle_state.tpms_pressure_fl | 2.9 |

- **fetched_at** is when Runsten read the response, in UTC; every value of one response shares it.
- **source** is where it came from: `volvo`, `polestar` or `tesla`; **endpoint** is the part of the provider's API it answered.
- **field** is the value's place in the response: its keys joined by dots, an element of a list as `[0]`, `[1]`…
- **value** is as the provider gave it: its units, its words, its numbers with a decimal point. A value it sent empty (`null`) is an empty cell, an empty group `{}` or `[]`.

Only the responses Runsten kept are in it: a response identical to the previous one of its endpoint is not kept again, so a value that did not change for an hour appears once, at its first reading.

**The fields are the providers' own**, not Runsten's. They may add, rename or drop one at any time: a field they add is in the file the day it comes. Their meaning is theirs to document; Runsten neither checks nor converts them (Tesla's distances are in miles and its pressures in bar, whatever the car displays).

What is secret or belongs to your account rather than to the car is left out: Tesla's tokens and its identifiers of your account and of the car in it. The VIN, the car's name and its positions stay: the file is yours, mind them before sharing it.

The period is a month at most: beyond it, or over the whole history, the button says so; choose a shorter period. A Tesla, read every 15 seconds while it drives, gives about 400,000 rows and 35 MB a day, past what a spreadsheet opens (about a million rows) within three days: open a longer one with a script or a tool such as DuckDB. A Volvo or a Polestar gives far fewer.

Programs read the same file through the API, with an access token: `GET /api/v1/vehicles/{vehicle}/readings.csv?from=…&to=…` ([API](../reference/api.md)).

## All of your account

In the settings, under **Account**, **Download my data** saves everything the account holds as one JSON file: the vehicles, the trips and charges, the places, tariffs and costs, and every response kept, raw. Secrets are left out (passwords, tokens, keys). It is open to every account, whatever its offer.

When the file outgrows the download — a Tesla, read every 15 seconds, gives a gigabyte a month — **Build a ZIP archive** prepares the same data in the background: the archive is built out of the request, however long it takes, the page telling how much of it is written. Once built, it is kept a week, or until another one is asked for, and can be downloaded again meanwhile; a download cut short resumes where it stopped, in a browser or a download manager that resumes.

Programs ask for it through the API, with an access token for the reads: `POST /api/v1/account/export-job` starts it, `GET /api/v1/account/export-job` reads how it stands, `GET /api/v1/account/export.zip` downloads the archive, a part of it with a `Range` header ([API](../reference/api.md)): `curl -L -C - -o runsten.zip …` takes up a download where it stopped. An instance that keeps its archives in a bucket answers with a link to it, valid a few minutes, which `-L` follows.

The command line does the same without the interface, against the database — the copy to take before a vehicle or an account is deleted:

```
runsten-api export [FILE]     # the single account, written as runsten-account-<day>.zip
runsten-api export --user NAME [FILE]     # the user's account, on an instance of several
runsten-api export --account ID [FILE]     # by its id, across every account
```

Docker Compose runs it as `docker compose run --rm api export`. The archive holds what the server's own build exports: a hosted offer's own tables are in its own server's books.
