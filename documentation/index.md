---
description: "What Runsten records of your electric car, the cars it reads, how it reads them, and what a self-hosted instance needs."
---

# What Runsten is

Runsten records the trips, charges and costs of your electric car, and shows them as a history and as statistics. It reads the car from its manufacturer, on a server of your own.

The name comes from runestones (*runsten* in Swedish), which often commemorated journeys.

::: warning Early development
Runsten works end to end, but is young: expect changes between versions. Polestar cars are read through Polestar's Data Portal, in beta until it is checked on real cars ([Your Polestar](./guide/polestar.md)). For those who don't want to run a server, a hosted version runs at [runsten.app](https://runsten.app), in beta; its [pricing](https://runsten.app/pricing) compares it with self-hosting.
:::

## Supported cars

- **Volvo**, through the Volvo Cars API: [Your Volvo application](./self-hosting/volvo.md).
- **Polestar** (beta), through Polestar's Data Portal: [Your Polestar](./guide/polestar.md).
- **Tesla** (beta), through the Owner API of the Tesla app, as TeslaMate reads it: [Your Tesla](./guide/tesla.md).

## How it works

- **The collector** polls the manufacturer's API (the Volvo Cars API, Polestar's Data Portal, Tesla's Owner API): every 10 minutes while the car is parked, every minute while it drives or charges; a Tesla at its own pace, so that it can sleep. It stores every response as it came, and the Volvo ID tokens, the Polestar credential and the Tesla refresh token encrypted.
- **Trips and charges** are worked out from those responses. A trip or a charge that happened while nothing could be read (an outage of the API, an exhausted quota) is still found, from the odometer or the state of charge, and marked as **reconstructed**. Since the responses are kept, the whole history can be worked out again after an update that improves it.
- **The web interface** shows the car's state, its trips and charges, their costs from your tariffs, statistics and an estimate of the battery's capacity. It works on a phone as well as a desktop, in English, French and Swedish, light or dark; on a phone, it installs on the home screen like an app ([Installing it on a phone](./guide/index.md#installing-it-on-a-phone)).
- **The API** (`/api/v1`) serves the same data to your own scripts.

## Figures you can trust

Runsten shows what it read, and when:

- **Times are ranges.** The car is read every few minutes: a trip started somewhere between two readings, and is shown so ("06:55–07:01 → 07:39–07:40").
- **Unknown is not zero.** A value the car did not give is shown as unknown, and left out of the totals.
- **Energies are estimates**, from the change of the state of charge and the battery's capacity. The pages say so.
- **A cost can be a range**, when a charge may fall on either side of a change of price.

## What you need

- A Linux server with Docker: a NAS, a small VPS, or a Raspberry Pi 4 or 5 on a 64-bit system.
- An electric car of a [supported brand](#supported-cars), with what its manufacturer asks for: for a Volvo, its Volvo ID, and a car sold in a market the Volvo Cars API serves (Volvo publishes no list of them: a car of another market is refused by every call, and Runsten says so on its pages); for a Polestar, a credential of your own on its Data Portal ([Your Polestar](./guide/polestar.md)); for a Tesla, a refresh token of your Tesla account ([Your Tesla](./guide/tesla.md)).
- To read a Volvo, an application of your own on the [Volvo Cars developer portal](https://developer.volvocars.com/), for the instance: see [Your Volvo application](./self-hosting/volvo.md). A Polestar or a Tesla needs none.

Then follow the [installation](./self-hosting/index.md).

---

Runsten is an independent project, not affiliated with Volvo Car Corporation, Polestar or Tesla. "Volvo", "Polestar" and "Tesla" are trademarks of their respective owners. It is licensed under the [AGPL-3.0-or-later](https://github.com/runsten-app/runsten/blob/main/LICENSE).
