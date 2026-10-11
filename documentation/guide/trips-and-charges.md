---
description: "Read the trips and charges of your Volvo in Runsten: times as ranges, reconstructed events and estimated energies."
---

# Trips and charges

The **Trips** and **Charges** tabs list the events of a period, by day, newest first, under a line of totals: "3 trips started in the period · 73 km · 18.8 kWh/100 km (estimated)". The period stays in the address of the page: a link to it opens the same list.

<Screen view="trips" alt="The Trips tab: the trips of a period, by day, under their totals." />

## Reading the times

The car is read every few minutes, never continuously. So a time is the two readings around it:

- **"06:55–07:01 → 07:39–07:40"**: the trip started between 06:55 and 07:01, and ended between 07:39 and 07:40.
- **A duration** is every value those bounds allow: "38–45 min".
- **A reconstructed event** was never seen: the car drove or charged while nothing could be read (an outage, an exhausted quota), and the odometer or the state of charge revealed it afterwards. It says so, "Happened between 12:00 and 13:00", and has no duration.

A value the car did not give says **Unknown**, never 0.

## Imported trips and charges

A vehicle's history may start before Runsten read it: trips and charges imported from another app ([Importing your history](./importing.md)) are marked with it, "Imported from Aximote" or "Imported from the Volvo Data Portal". Only what started before Runsten's first reading of the vehicle is imported, so they never duplicate the events Runsten saw. Their times and figures are the app's own: one time at each end, never a range, and the energy and cost it gave, when it gave them. A charge's cost is the app's when it is in your currency, else its place's tariff's.

An imported trip with a track draws it on its map, where your instance shows maps and you have not hidden them; otherwise, as a trip Runsten read, it has its two ends only: Volvo gives no position while driving.

They count in the lists and the statistics like the others. The vehicle's settings list them by app, under **Imported history**, and delete one app's at once; what Runsten read stays.

## Energies

The energy of a trip or a charge is an estimate: the change of the state of charge times the battery's capacity. Runsten uses the net (usable) capacity of the vehicle's model when it knows it, else the capacity Volvo reports ([Vehicle models](./vehicle-models.md)). Each trip and charge tells which.

## A charge

<Screen view="charge" alt="A charge at home: its energy, its place and its cost." />

A charge shows its type (AC or DC), its energy, its place (or "Outside any place") and, once a currency is chosen, its cost. See [Places, tariffs and costs](./costs.md).
