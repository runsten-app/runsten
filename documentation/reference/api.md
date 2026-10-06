---
description: "The JSON API of Runsten: signing in, the routes, units, time bounds, costs and errors."
---

# API

`runsten-api` serves a JSON API under `/api/v1`, for the web interface and for your scripts: it reads the vehicles and their events, and writes the account's settings and the costs of charges.

The API is described by an OpenAPI 3.1 document, [`api/openapi.yaml`](https://github.com/runsten-app/runsten/blob/main/api/openapi.yaml): routes, parameters, schemas, error codes, and what the fields mean. Clients can generate their types from it, for instance with `npx openapi-typescript api/openapi.yaml -o api.ts`.

## Signing in

Every route requires a session: sign in with `POST /api/v1/session`, which sets the session cookie, and send the cookie back.

```sh
curl -c jar -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}' http://127.0.0.1:8081/api/v1/session
curl -b jar http://127.0.0.1:8081/api/v1/vehicles
```

A request body is sent as `application/json`.

## Routes

| Route | |
|---|---|
| `GET`, `POST`, `DELETE /api/v1/session` | the current session; sign in; sign out |
| `GET /api/v1/vehicles`, `…/vehicles/{id}` | the vehicles, their model, the state of their Volvo connection and what the collector did on its latest pass |
| `GET /api/v1/vehicles/{id}/variants` | the catalog's variants of the vehicle's family, the candidates of the recognition first |
| `PUT /api/v1/vehicles/{id}/model` | choose the variant and the onboard charger (`null` for the recognition, and for a charger not stated) |
| `GET /api/v1/vehicles/{id}/state` | the latest known values: SoC, range, odometer, position, charging, each with when it was read |
| `GET /api/v1/vehicles/{id}/trips`, `…/charges` | newest first, by pages (`limit`, `cursor`), within a period (`from`, `to`) |
| `GET /api/v1/vehicles/{id}/trips/{trip}`, `…/charges/{charge}` | one trip or charge |
| `PUT`, `DELETE /api/v1/vehicles/{id}/charges/{charge}/cost` | enter what a charge cost; delete it |
| `GET /api/v1/vehicles/{id}/stats` | totals of a period (`from`, `to`), by `day`, `week` or `month` in a time zone (`bucket`, `tz`) |
| `GET /api/v1/vehicles/{id}/battery` | the estimated battery capacity over the whole history |
| `GET`, `PUT /api/v1/settings` | the account's currency, and the accepted ones |
| `GET`, `POST /api/v1/places` | the places, with their tariffs; create one |
| `GET`, `PUT`, `DELETE /api/v1/places/{place}` | one place; replace it whole, tariff included; delete it |
| `GET /api/v1/places/{place}/unpriced` | the place's charges that started before its tariff's first day |
| `GET /api/v1/charge-costs/orphans` | the entered costs that no charge takes any more |
| `PUT /api/v1/charge-costs/orphans/{cost}/charge`, `DELETE …/orphans/{cost}` | attach one to a charge; delete it |

## Conventions

- **Units are in the field names**: `_km`, `_m`, `_kwh`, `_pct`, `_w`, `_kw`, `_s`; `_minor` in minor units of the account's currency (cents), `price_per_kwh` in its major units.
- **Times** are RFC 3339 in UTC. Trip and charge times are bounds: `start.after`, `start.before`, `end.after`, `end.before`. `reconstructed` marks the events that were never seen.
- **Unknown is `null`**, never zero.
- **Energies** are estimates: ΔSoC × `capacity_kwh`, with `capacity_source` `catalog_net` (the net capacity of the variant) or `api` (the capacity Volvo reports).
- **Durations** are `{"min", "max"}` in seconds.
- **A cost** is `{"currency", "min_minor", "max_minor", "source", …}`, `min_minor = max_minor` when it is one amount; `source` is `tariff` or `entered`.
- **Statistics** count each event once, in the interval that holds its `start.after`; a sum's `_unknown` companion counts the events that lacked a value. At most 400 intervals; `tz` is an IANA time zone, `UTC` by default.
- **Errors** are `{"error": {"code": …, "message": …}}`.
