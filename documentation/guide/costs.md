---
description: "Places, tariffs with off-peak hours, the cost of each charge as a range, entering the cost from a receipt, and the cost of each trip."
---

# Places, tariffs and costs

Costs appear once a currency is chosen in **Settings → Costs**. Every price and cost is in it, without conversion; once a price or an entered cost exists, it can no longer change.

## Places

A place is a circle, a position and a radius (100 m by default), with a tariff: home, work, a charging station you use. A charge takes the tariff of the nearest place that holds its position: the first one the car reports after the charge ends, within 15 minutes and before it drives off or another charge starts, else the last one it reported before. There can be up to 100 places.

- The position is typed, taken from a vehicle's current position, or, where the instance shows maps ([Maps](../self-hosting/maps.md)), picked on the map.
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

A charge's cost is estimated from its place's tariff: the energy drawn from the grid is the energy the battery took over the efficiency, as the charge says: "30 kWh billed (estimated), assuming an efficiency of 88%". The energy the battery took is the one the vehicle measured, for a car that measures it (a Tesla); else the change of the state of charge, or, when it rose by less than 2 points and the car reported its charging power during the charge, that power added up (for an imported charge, the energy the app gave): a small charge barely moves a state of charge counted in whole percent, and 0.7 kWh into a large battery may read 66 % → 66 %.

When the car's maker records the energy each charging session drew from the grid (a BMW or a Mini, read once a day: [BMW](./bmw.md#charging-history)), the cost rests on it as it is, with no efficiency between: "31 kWh billed, as recorded by the maker". It shows on the charge, with the losses: the difference with the energy the battery took.

The cost is a range when the charge's times allow several prices ("€5.24 – €5.79"), one amount otherwise. An unknown cost says so, and a cost of exactly 0 is "Free".

The power bounds how much energy each price can take, and narrows the range: the place's charger, and for an AC charge the car's onboard charger ([Vehicle models](./vehicle-models.md#onboard-charger)).

## Entering a cost

**Enter the cost** records what was paid, as on the receipt: the amount, and optionally the billed energy and a note. It replaces the tariff's estimate until it is deleted, and survives a rebuild.

## The cost of a trip

A trip costs its energy at the average price of the kWh that went into the battery over the **30 days before it**: what the vehicle's charges that started then cost, over the energy they added. Both are on the battery's side, so the losses of charging are in the price: "6 kWh at €0.48/kWh, €2.90". A trip after a week of fast charging costs more than one after a week at home, as on the bills; and a trip keeps its cost whatever period you look at.

- Every charge with a cost counts: from a tariff, entered, or imported. A charge without a cost, or without an energy, is left out of the average, and the trip says how many.
- When no charge of those 30 days has a cost, the trip's cost is unknown, never zero. So is the cost of a trip whose energy is unknown (one Runsten found from the odometer alone).
- The cost is shown as one amount when its lowest and highest lie within 10 % of each other, else as a range: the charges' costs it rests on are ranges.
- With a distance, the trip also tells its cost per 100 km.

The trip's page has a **Cost** card: the amount, the price per kWh, how many charges it rests on, and where their energy was charged: at each of your places, then AC and DC elsewhere, each with its price.

A plug-in hybrid's trip costs the electricity it used only: the fuel is not counted, and it has no cost per 100 km, as it has no consumption.

## Costs without a charge

When the charges are worked out again (after an update or a rebuild), an entered cost may no longer match exactly one charge. It is never lost: **Settings → Costs**, under **Costs without a charge**, lists it, to attach to one of the charges around its time, or delete. Until then, the statistics show it apart, never in the total, where it would count its charge twice.
