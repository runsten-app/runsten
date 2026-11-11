---
description: "The Garage: the vehicles of an account side by side, their totals, their consumption, cost, charging and battery compared, and the spending per month."
---

# The Garage

An account with two vehicles or more opens on the **Garage**: its vehicles side by side, over the same period. With a single vehicle, Runsten opens that vehicle's page, as before. **All vehicles**, first in the vehicle menu of the bar, leads back to the Garage from any page.

The period is the **last 30 days** or the **last 12 months** (this month and the eleven before). The totals, the tiles and the comparison follow it.

## The totals

At the top, the account's figures over the period, every vehicle added up: the distance driven, what the electricity cost, and what 100 km cost on average. Above them, how many vehicles are driving or charging right now. A cost needs the account's currency ([Places, tariffs and costs](./costs.md)); the average per 100 km is unknown as soon as a charge's cost or a trip's distance is, since a part left out would bend it.

## The vehicles

A tile per vehicle, in its color in the charts: its state of charge and range, what it does now (parked, charging to its target, driving), and over the period its distance, its consumption and what 100 km cost it. A state no longer current (not read for two hours, or its connection lost) says when it was read instead, and counts in nothing "now". The lowest consumption and the lowest cost per 100 km among the vehicles compared are marked **Lowest**; a consumption estimated over part of the trips only (some without a known energy or distance) is shown, never marked. A vehicle that drove nothing over the period says so, and is not ranked. Select a tile to open the vehicle's page.

**Hide from the comparison** takes a vehicle out of the charts and of the marks, for a car that is never driven or one you do not want to compare; its tile stays, faded, and the same button brings it back. Hiding lasts for the visit only. The totals and the spending stay the account's, hidden vehicles included.

## Compare

The vehicles compared, over the same period, one color each:

- **Consumption**: the consumption by outside temperature, by band of 5 °C, a line per vehicle. The cold raises the consumption of every car: compared band by band, two cars driven in different seasons compare fairly. It needs a car that tells its outside temperature (a [Tesla](./tesla.md#its-temperatures)); the others are named under the chart, not drawn ([Consumption by outside temperature](./statistics.md#consumption-by-outside-temperature)).
- **Cost**: what 100 km cost each vehicle, month by month over the last 12 months, whatever the period. A cost known as a range (a charge whose tariff changed during the charge) is drawn at its lowest.
- **Charging**: the share of each vehicle's energy charged at your places, on AC elsewhere and on DC. Your places are those of the settings, with their tariffs: they explain most differences in cost. A charge Runsten did not see, or one without a position outside your places, counts as unknown.
- **Battery**: the charge lost per day while parked over the period ([The loss while parked](./statistics.md#the-loss-while-parked)), and the change of the estimated capacity since the vehicle's first estimates, over its whole history ([Battery](./battery.md)). Read both with the cars' ages: a capacity drops faster in the first months.

## Spending per month

What the electricity of every vehicle cost, month by month over the last 12 months, a bar a month stacked by vehicle. A month whose costs are all unknown has no part for that vehicle, never a zero; charges of unknown cost beside known ones are left out, and the chart says how many.

Where an instance limits the statistics by interval, the totals, the tiles and the comparison by temperature, by charging and by battery stay; the cost by month and the spending per month say they are unavailable.
