---
description: "How Runsten estimates the remaining capacity of the battery from its charges: an estimate among the vehicle's health figures, not a measurement."
---

# Battery

The battery page estimates the remaining capacity of the battery, from its charges. It is reached from the **Health** tab, whose battery tile gives the estimate: one of the [health](./health.md)'s figures.

<Screen view="battery" alt="The battery page: the estimated capacity against the data sheet, and its estimates over time." />

- **The gauge** compares the current estimate with the capacity of the model's data sheet, with its margin.
- **The chart** shows each charge's estimate against the date or the mileage, with the monthly median and its middle half.
- **The evolution** over the months appears once there are 12 months of history and enough estimates.
- **The range at 100 %** is the car's own forecast; it follows the season and the driving, never the battery alone. A Tesla's is its rated range, which follows the battery ([A Tesla](#a-tesla)).

## How it is estimated

Each charge gives an estimate: the energy it took, over the rise of the state of charge it gave. The energy is the charging power the car reports, added up over the charge, or the energy billed on a receipt you entered. The current capacity is the median of the latest 20 estimates.

::: warning An estimate, not a measurement
The car exposes no energy meter, and its state of charge is a whole number of percent. A single estimate is approximate; the trend over the months tells more than the level. Some charges are set aside, and the page says how many and why: a rise of less than 20 points, a slow charge (under 2 kW), readings too far apart, a charge above 95 %, an estimate far from the data sheet, or a reconstructed charge.
:::

## A Tesla

A Tesla tells more: the energy it counts into its battery during a charge, its rated range, a fixed conversion of the charge it can use, and that usable charge, which the cold lowers below the state of charge. Runsten reads them as TeslaMate does:

- **The rated efficiency**, the energy a kilometre of rated range holds: the energy the car measured over the rated range a charge added, the median over the charges of 10 minutes or more that end at 95 % at most. The capacity tile gives it.
- **An estimate per charge**: the rated range at its end, brought back to 100 % of the usable charge, times the rated efficiency. A charge that ends with less than 50 % usable gives none: its whole-number percent weighs too much.

The car's own count is far more precise than the rise of its state of charge. Once there are 5 of these estimates, the current capacity, its evolution and the monthly median rest on them alone; the others stay on the chart, in another color. It is still the battery's estimate of itself, not a test of the battery.
