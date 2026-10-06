---
description: "Places, tariffs with off-peak hours, the cost of each charge as a range, and entering the cost from a receipt."
---

# Places, tariffs and costs

Costs appear once a currency is chosen in **Settings**. Every price and cost is in it, without conversion; once a price or an entered cost exists, it can no longer change.

## Places

A place is a circle, a position and a radius (100 m by default), with a tariff: home, work, a charging station you use. A charge takes the tariff of the nearest place that holds it. There can be up to 100 places.

- The position is typed, or taken from a vehicle's current position. No map is loaded.
- A charge outside every place offers **Create a place here**, at its position.
- A place may also take the AC charges that have no position (one place at most).
- A place may state its charger's power, which narrows the cost of a charge across several prices, and its charging efficiency (by default 88 % in AC, 95 % in DC).

## Tariffs

A tariff is a list of prices, each from a day on, in the place's time zone. **New price from…** copies the latest one.

- A price has a base price per kWh, and time windows with their own price: by day of the week, hours and months. Off-peak hours are a window.
- A window may cross midnight: 22:00–06:00 on Monday is the night from Monday to Tuesday. Where windows overlap, the last one wins.
- A new price leaves the past charges at theirs; correcting a price changes them.
- A tariff applies from its first day on, never before. When charges at the place started earlier, its page says how many, and offers to start the tariff on the day of the first.

## The cost of a charge

A charge's cost is estimated from its place's tariff: the energy drawn from the grid is the energy the battery took over the efficiency, as the charge says: "30 kWh billed (estimated), assuming an efficiency of 88%".

The cost is a range when the charge's times allow several prices ("€5.24 – €5.79"), one amount otherwise. An unknown cost says so, and a cost of exactly 0 is "Free".

The power bounds how much energy each price can take, and narrows the range: the place's charger, and for an AC charge the car's onboard charger ([Vehicle models](./vehicle-models.md#onboard-charger)).

## Entering a cost

**Enter the cost** records what was paid, as on the receipt: the amount, and optionally the billed energy and a note. It replaces the tariff's estimate until it is deleted, and survives a rebuild.

## Costs without a charge

When the charges are worked out again (after an update or a rebuild), an entered cost may no longer match exactly one charge. It is never lost: **Settings → Costs without a charge** lists it, to attach to one of the charges around its time, or delete. Until then, the statistics show it apart, never in the total, where it would count its charge twice.
