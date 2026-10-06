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

## Energies

The energy of a trip or a charge is an estimate: the change of the state of charge times the battery's capacity. Runsten uses the net (usable) capacity of the vehicle's model when it knows it, else the capacity Volvo reports ([Vehicle models](./vehicle-models.md)). Each trip and charge tells which.

## A charge

<Screen view="charge" alt="A charge at home: its energy, its place and its cost." />

A charge shows its type (AC or DC), its energy, its place (or "Outside any place") and, once a currency is chosen, its cost. See [Places, tariffs and costs](./costs.md).
