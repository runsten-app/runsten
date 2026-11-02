---
description: "Distance, consumption, energy charged and cost by day, week or month, and how Runsten counts each event."
---

# Statistics

The **Stats** tab shows the totals of a period, this month by default, a shortcut or any dates, and charts by day, week or month:

- the distance and the driving time;
- the odometer at the end of each interval;
- the consumption;
- the energy charged, by AC and by DC;
- with a currency, the cost of the charges: the lowest cost, with its uncertainty up to the highest;
- the charge lost while parked, with the power it stands for and, for a car that tells it, the share of the time it slept.

<Screen view="stats" alt="The Stats tab: distance, consumption, energy charged and cost, by day." />

Selecting a bar opens the trips or charges of its interval; a place's bar, the charges of that place over the period ([Charges by place](./trips-and-charges.md#charges-by-place)).

## Consumption by outside temperature

For a car that tells its outside temperature (a [Tesla](./tesla.md#its-temperatures)), the driving view adds the consumption of its trips by band of 5 °C, from the coldest band holding a trip to the warmest: an electric car uses more in the cold, heating its battery and its cabin. Point at a band to see its trips, their distance and their average speed: a winter in town and a summer on the motorway differ by more than the temperature, and the speed tells them apart. The speed is a range, from its lowest to its highest, since the times of a trip are known within the minute Runsten reads the car. The table under the chart, for screen readers, holds the same figures.

Each trip counts in the band of its average outside temperature. A trip without one is left out, and the chart says how many: one Runsten did not see (found from the odometer), an imported one, or one recorded before Runsten kept the temperature, until the instance rebuilds its trips and charges. So is a temperature below -60 °C or from 70 °C, taken for a wrong reading. A Volvo or a Polestar tells no temperature: their statistics have no such chart.

## The loss while parked

The charge lost while parked is the drop of the state of charge between two events (a trip, a charge), per day. The tile adds the mean power it stands for, in watts: that drop as energy, over the time parked, when the battery's capacity is known. For a car that tells whether it sleeps (a [Tesla](./tesla.md#letting-the-car-sleep)), it adds the share of the parked time it slept or was out of reach: a car that loses 1 % a day asleep differs from one an application, a Sentry Mode or a climate keeps awake. That share covers the time Runsten read the car's state; it never wakes the car to read it.

## The cold, parked and charging

For a car that tells its outside temperature, the charging view adds two charts by band of 5 °C, as the consumption's:

- **the loss while parked**, per day, by the temperature of the parked intervals; point at a band for the power lost, the share of the time asleep and the number of intervals. A car asleep is not read, and waking it to read would itself drain it: an interval takes the average temperature of the trip or charge before it and of the one after it, those that tell one. An interval between two events without a temperature is left out, and the chart says how many.
- **the charging power**, AC and DC apart, by the average outside temperature of the charges: a cold battery charges slower, warmed first. A charge's power is its energy (from the charging power the car reports) over its time, its pauses included, known within the minute Runsten reads the car: the bars are the lowest power those times allow, and pointing at a band gives both bounds. A charge Runsten did not see, an imported one, or one recorded before Runsten kept the temperature is left out, until the instance rebuilds its trips and charges.

The temperature is the one the car measures where it stands, which in a garage or an underground car park is not the weather outside. A Volvo or a Polestar tells none: their statistics have neither chart.

## How the figures are counted

- **Each trip and charge counts once**, in the interval it started in, never shared between two. The lists of trips and charges, instead, show every event that may overlap their period: the totals of a list and of a chart may differ for an event that crosses midnight or the end of a month.
- **A sum covers the known values**, and says how many events lacked one. An average over no known value is unknown.
- **The odometer** is read from the trips (at their start and end) and the charges (at their end). It only moves while driving, so it holds over a day without a trip. The distance it tells over the period may differ from the sum of the trips: a trip whose distance is unknown is in the one, not in the other.
- **Energies, consumption and the loss while parked are estimates** from the state of charge; the page says so.
- **A plug-in hybrid has no consumption**: its trips' energy is the battery's alone, whatever the engine drove ([Plug-in hybrids](./vehicle-models.md#plug-in-hybrids)).
- **Days, weeks and months** are those of your browser's time zone. Weeks start on Monday.
