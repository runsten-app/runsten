---
description: "A vehicle's health in Runsten: its battery's estimated capacity, its tyres and their pressures over time, its next service, its warnings, and what Volvo and Polestar each report."
---

# Health

The **Health** tab shows the vehicle's latest health and the tyres' pressures over time. A dot on the tab, from every page of the vehicle, says that something needs your attention: a tyre, the service, a warning. Runsten reads it every hour: it changes slowly.

<Screen view="health" alt="The Health tab: the battery, the tyres, the service and the warnings." />

- **Battery**: its estimated capacity, which leads to the [battery page](./battery.md): its estimates and their evolution.
- **Tyres**: each tyre's pressure, against the pressure the car recommends at its axle, and what the tyre pressure monitoring says of it: OK, low, very low, high, or a fault (of a sensor, or of the system).
- **Service**: whether it is due (not yet, soon, due, overdue), and the time and the distance left until it.
- **Warnings**: the brake fluid, the coolant, the oil, the washer fluid, the 12 V battery and each exterior light. The active ones are named; the others are counted.
- **The pressures over time**, one chart per axle: the left and right tyres, and the recommended pressure, dashed, over 7 days, 30 days or 12 months. A pressure rises as the tyres warm up while driving, and falls in the cold. The line breaks where nothing was read.

What a car does not report is said as such, never shown as fine.

## What each car tells

| | Volvo | Polestar |
|---|---|---|
| Tyres | a warning per tyre, no pressure | the pressures and the warnings, when the model gives them |
| Service | the months and the distance left | the days and the distance left |
| Warnings | the fluids and the lights | the fluids, the 12 V battery and the lights |

- A **Volvo**'s health comes from the Connected Vehicle API's tyres, warnings and diagnostics, which Runsten reads every hour already: it costs no more calls. Without a pressure, its page has no chart.
- A **Polestar**'s comes from the Data Portal's **health** domain, optional: grant it to your credential ([Your Polestar](./polestar.md)), else the page says the car told nothing of it. The Polestar 2 we have seen a response of reports no tyre; which models give their pressures is not known yet. Tell us what yours gives.

::: info History
Runsten keeps every response it reads, and reads the health from them: what it learns later of a car's format applies to the whole history.
:::

## Home Assistant

The health is published to your MQTT broker with the rest of the state: the pressures as sensors, the warnings as *problem* binary sensors ([Home Assistant and MQTT](./mqtt.md)).
