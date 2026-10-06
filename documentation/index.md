---
description: "What Runsten records of a Volvo electric car, how it reads it, and what a self-hosted instance needs."
---

# What Runsten is

Runsten records the trips, charges and costs of Volvo electric cars, and shows them as a history and as statistics. It reads the car through the Volvo Cars API, on a server of your own.

The name comes from runestones (*runsten* in Swedish), which often commemorated journeys.

::: warning Early development
Runsten works end to end, but is young: expect changes between versions. Polestar cars are in its catalog of models, but no data source reports one yet. A hosted version, for those who don't want to run a server, will come later.
:::

## How it works

- **The collector** polls the Volvo API: every 10 minutes while the car is parked, every minute while it drives or charges. It stores every response as it came, and the Volvo ID tokens encrypted.
- **Trips and charges** are worked out from those responses. A trip or a charge that happened while nothing could be read (an outage of the API, an exhausted quota) is still found, from the odometer or the state of charge, and marked as **reconstructed**. Since the responses are kept, the whole history can be worked out again after an update that improves it.
- **The web interface** shows the car's state, its trips and charges, their costs from your tariffs, statistics and an estimate of the battery's capacity. It works on a phone as well as a desktop, in English, French and Swedish, light or dark.
- **The API** (`/api/v1`) serves the same data to your own scripts.

## Figures you can trust

Runsten shows what it read, and when:

- **Times are ranges.** The car is read every few minutes: a trip started somewhere between two readings, and is shown so ("06:55–07:01 → 07:39–07:40").
- **Unknown is not zero.** A value the car did not give is shown as unknown, and left out of the totals.
- **Energies are estimates**, from the change of the state of charge and the battery's capacity. The pages say so.
- **A cost can be a range**, when a charge may fall on either side of a change of price.

## What you need

- A Linux server with Docker: a NAS, a small VPS, or a Raspberry Pi 4 or 5 on a 64-bit system.
- A Volvo electric car and its Volvo ID.
- An application of your own on the [Volvo Cars developer portal](https://developer.volvocars.com/): see [Your Volvo application](./self-hosting/volvo.md).

Then follow the [installation](./self-hosting/index.md).

---

Runsten is an independent project, not affiliated with Volvo Car Corporation or Polestar. "Volvo" and "Polestar" are trademarks of their respective owners. It is licensed under the [AGPL-3.0-or-later](https://github.com/runsten-app/runsten/blob/main/LICENSE).
