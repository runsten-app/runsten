---
description: "How Runsten estimates the remaining capacity of the battery from its charges, and why it is not a health measurement."
---

# Battery

The **Battery** tab estimates the remaining capacity of the battery, from its charges.

<Screen view="battery" alt="The Battery tab: the estimated capacity against the data sheet, and its estimates over time." />

- **The gauge** compares the current estimate with the capacity of the model's data sheet, with its margin.
- **The chart** shows each charge's estimate against the date or the mileage, with the monthly median and its middle half.
- **The evolution** over the months appears once there are 12 months of history and enough estimates.
- **The range at 100 %** is the car's own forecast; it follows the season and the driving, never the battery alone.

## How it is estimated

Each charge gives an estimate: the energy it took, over the rise of the state of charge it gave. The energy is the charging power the car reports, added up over the charge, or the energy billed on a receipt you entered. The current capacity is the median of the latest 20 estimates.

::: warning An estimate, not a health measurement
The car exposes no energy meter, and its state of charge is a whole number of percent. A single estimate is approximate; the trend over the months tells more than the level. Some charges are set aside, and the page says how many and why: a rise of less than 20 points, a slow charge (under 2 kW), readings too far apart, a charge above 95 %, an estimate far from the data sheet, or a reconstructed charge.
:::
