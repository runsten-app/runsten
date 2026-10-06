---
description: "How Runsten recognizes the variant of your Volvo, choosing it, the onboard charger and the net battery capacity."
---

# Vehicle models

Runsten carries a catalog of the electric Volvo and Polestar variants: for each, its model years, its gross and net battery capacities, its onboard charger (standard, and the optional 22 kW one), its DC power, and the sources of every figure. It comes with Runsten: an update brings its corrections.

The model matters for two things: the net capacity, on which energies rest, and the onboard charger, which bounds the cost of an AC charge.

## Recognition

The variant is worked out from the details the Volvo API reports and the VIN; nothing more is collected. The vehicle is recognized when exactly one variant fits, and only a fully electric one: a plug-in hybrid never is.

When several variants fit, or none, the vehicle has no variant until you choose one: its energies rest on the capacity Volvo reports.

## Choosing

In the vehicle's settings (the button by its name), choose the variant among those of its family, the candidates of the recognition first. Each shows its capacities and powers, and whether they come from the manufacturer or from public sources.

The choice outranks the recognition; **Automatic recognition** goes back to it.

## Onboard charger

When the variant offers a 22 kW onboard charger as an option, state which one the car has. Unstated, 22 kW is assumed: a higher bound never narrows a cost by mistake. State 11 kW if the car has the standard one.

## What a choice changes

- **A charger** applies at once: costs are computed on each reading.
- **A variant** changes the energies, and with them consumptions and costs. The collector works out the vehicle's trips and charges again after its next reading (within 10 minutes while parked), keeping the entered costs.

## Net capacity

Volvo reports the gross capacity of the battery. Runsten assumes, until a measurement confirms it, that the state of charge runs over the net (usable) capacity, and uses it when the catalog knows it. For an EX30 of 69 kWh gross, 64 net, energies are 7 % lower than on the gross capacity.

A missing or wrong variant? [Contribute it](../contributing/variants.md).
