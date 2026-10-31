---
description: "Read the trips and charges of your car in Runsten: times as ranges, reconstructed events and estimated energies."
---

# Trips and charges

The **Trips** and **Charges** tabs list the events of a period, by day, newest first, under a line of totals: "3 trips started in the period · 73 km · 18.8 kWh/100 km (estimated)". The period stays in the address of the page: a link to it opens the same list.

<Screen view="trips" alt="The Trips tab: the trips of a period, by day, under their totals." />

## Reading the times

The car is read every few minutes, never continuously. So a time is the two readings around it:

- **"06:55–07:01 → 07:40"**: the trip started between 06:55 and 07:01, and ended at 07:40. Two readings less than a minute apart give one time, the nearest minute to their middle: the minutes shown would tell nothing finer.
- **A Tesla** parked a few minutes is left alone for 15 minutes, so that it may fall asleep: reading it keeps it awake and drains its battery. A trip started meanwhile is seen at its first reading on the way, and its start spans the window: "20:35–20:50".
- **A duration** is every value those bounds allow: "38–45 min".
- **A trip that never moved**, the car put in drive and back without its odometer or its position changing (to unplug, say), is not a trip.
- **A reconstructed event** was never seen: the car drove or charged while nothing could be read (an outage, an exhausted quota), and the odometer or the state of charge revealed it afterwards. It says so, "Happened between 12:00 and 13:00", and has no duration.

A value the car did not give says **Unknown**, never 0.

## Imported trips and charges

A vehicle's history may start before Runsten read it: trips and charges imported from another app ([Importing your history](./importing.md)) are marked with it, "Imported from Aximote" or "Imported from the Volvo Data Portal". Only what started before Runsten's first reading of the vehicle is imported, so they never duplicate the events Runsten saw. Their times and figures are the app's own: one time at each end, never a range, and the energy and cost it gave, when it gave them. A charge's cost is the app's when it is in your currency, else its place's tariff's.

An imported trip with a track draws it on its map, where your instance shows maps and you have not hidden them; otherwise, as a trip Runsten read, it has its two ends only: Runsten draws no track of the trips it reads.

They count in the lists and the statistics like the others. The vehicle's settings list them by app, in **Imported history**, and delete one app's at once; what Runsten read stays.

## Energies

The energy of a trip or a charge is an estimate: the change of the state of charge times the battery's capacity. Runsten uses the net (usable) capacity of the vehicle's model when it knows it, else the capacity the car reports, if it reports one ([Vehicle models](./vehicle-models.md)). Each trip and charge tells which.

A Tesla also measures the energy that goes into its battery during a charge: the charge shows it first, measured by the vehicle, beside the two estimates.

## A charge

<Screen view="charge" alt="A charge at home: its energy, its place and its cost." />

A charge shows its type (AC or DC), its energy, its place (or "Outside any place") and, once a currency is chosen, its cost. See [Places, tariffs and costs](./costs.md).

## A charge in progress

While the car charges, the **Charges** tab shows the charge first, under **Now**, marked **In progress**: since when, the state of charge from its start to now, the energy added so far and, at a place with a tariff, its cost so far. It leads to its details, which the vehicle's page links to as well, under **Charging**, read again every minute: the power now, the energies so far, the cost so far and the charge curve up to the latest reading. It is not yet a charge of the list: the totals and the statistics count it once it is over. Then the same page shows the charge as it ended, with its end and its duration, without leaving it.

If nothing has been read for 15 minutes (the collector stopped, or lost the car), it no longer says **In progress** but **Readings stopped**, and when it was last read: the charge may have ended since, unseen. It shows the charge as it was at that reading, never as if it went on.

A charge that stops with the cable still connected, and starts again before it is unplugged, is one charge: a solar charge, which stops and starts again with the sun, or a charge the car tops up later. Until it starts again, it shows as ended where it stopped. Unplugged, driven, or stopped for 24 hours or more, the next charge is a new one.
