# Runsten

[![ci](https://github.com/runsten-app/runsten/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/runsten-app/runsten/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/runsten-app/runsten?include_prereleases&sort=semver)](https://github.com/runsten-app/runsten/releases)
[![licence](https://img.shields.io/github/license/runsten-app/runsten)](LICENSE)

> Open source data logger for Volvo and Polestar EVs.

**[runsten.app](https://runsten.app)** · [Documentation](https://runsten.app/docs/) · [Self-hosting](https://runsten.app/docs/self-hosting/)

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://runsten.app/screens/state-dark-desktop.png">
  <img alt="The state of a vehicle in Runsten: state of charge, range, charging, odometer and last position" src="https://runsten.app/screens/state-light-desktop.png">
</picture>

Runsten records the trips, charges and costs of Volvo electric cars, and shows them as a history and as statistics. It reads the car through the Volvo Cars API, on a server of your own.
The name comes from runestones (*runsten* in Swedish), which often commemorated journeys: Runsten carves your car's trips in stone.

- **A collector** polls the Volvo API and keeps every response as it came; trips and charges are worked out from them, including those that happened while nothing could be read, marked as reconstructed.
- **A web interface**, on a phone as well as a desktop, in English, French and Swedish: the car's state, its trips and charges, their costs from your tariffs, statistics, and an estimate of the battery's capacity.
- **An API** (`/api/v1`) and **MQTT** publishing, with Home Assistant's discovery, for your own scripts and home automation.

Status: **early development**. Expect changes between versions.

## Self-hosting

Runsten runs with Docker Compose on any Linux server: a NAS, a small VPS, a Raspberry Pi 4 or 5. It needs an application of your own on the [Volvo Cars developer portal](https://developer.volvocars.com/).
The [documentation](https://runsten.app/docs/) covers [installing](https://runsten.app/docs/self-hosting/), [the Volvo application](https://runsten.app/docs/self-hosting/volvo), [updating](https://runsten.app/docs/self-hosting/updating), [backups](https://runsten.app/docs/self-hosting/backup) and [the API](https://runsten.app/docs/reference/api); its sources are in [`documentation/`](documentation/).

## Development

A simulator of the Volvo API makes it possible to work without a car. [`DEVELOPMENT.md`](DEVELOPMENT.md) explains how to set Runsten up, run and test it, and how releases are made; [`ARCHITECTURE.md`](ARCHITECTURE.md) how the code is organized; [`CONTRIBUTING.md`](CONTRIBUTING.md) how to propose a change. In short:

```sh
task web-install     # once: the front end's dependencies (Node of web/.nvmrc)
tilt up              # the whole stack on the simulator: http://127.0.0.1:5173 (admin / runsten-dev-password)
task unit            # format, lint, tests and build, before a pull request
```

## Licence

[AGPL-3.0-or-later](LICENSE).

---

Runsten is an independent project, not affiliated with Volvo Car Corporation or Polestar. "Volvo" and "Polestar" are trademarks of their respective owners.
