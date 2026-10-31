---
description: "How Runsten recognizes the variant of your car, choosing it, the onboard charger, its sheet and the net battery capacity."
---

# Vehicle models

Runsten carries a catalog of the variants of the brands it reads: the electric Volvos and Polestars, the European Tesla Model 3 and Model Y, and the plug-in hybrids (the T6 and T8 Volvos of the 60 and 90 series, the T4 and T5 of the XC40, and the Polestar 1): for each, its model years, its gross and net battery capacities, its onboard charger (standard, and the optional 22 kW one), its DC power (most plug-in hybrids have none), and the sources of every figure. It comes with Runsten: an update brings its corrections.

The model matters for two things: the net capacity, on which energies rest, and the onboard charger, which bounds the cost of an AC charge.

## Recognition

The variant is worked out from what the manufacturer's API reports of the car and its VIN; nothing more is collected. What that is depends on the brand, below. The vehicle is recognized when exactly one variant fits, among those of its powertrain: a plug-in hybrid among the plug-in hybrids, an electric car among the electric ones, so that a hybrid is never taken for an electric car of a battery nearby. A car whose fuel type is not read yet, or that runs on fuel alone, has no variant.

When several variants fit, or none, the vehicle has no variant until you choose one: its energies then rest on the capacity the car reports, if it reports one (a Volvo does, a Polestar or a Tesla does not).

A Volvo tells its model, its model year, its fuel type and a capacity of its battery: it is recognized from them.

A Polestar read through the Data Portal tells its model year (from its VIN), but neither its model nor its battery's capacity: several Polestar variants fit, and none is recognized. Choose yours: without a variant, its energies are unknown ([Your Polestar](./polestar.md#choose-its-variant)).

A Tesla tells its model, its model year (from its VIN) and its trim badge, but not its battery's capacity: it is recognized when exactly one variant fits its model, year and badge, a variant that lists no badge always fitting. Otherwise, choose yours among those that fit: until you do, its trips and charges have no energy ([Your Tesla](./tesla.md#its-variant)).

## Choosing

In the vehicle's settings (the menu of its name in the bar), choose the variant among those of its family (of its brand, for a Polestar of the Data Portal: the variant chosen sets its model), the candidates of the recognition first. Each shows its capacities and powers, and whether they come from the manufacturer or from public sources.

The choice outranks the recognition; **Automatic recognition** goes back to it.

## Onboard charger

When the variant offers a 22 kW onboard charger as an option, state which one the car has. Unstated, 22 kW is assumed: a higher bound never narrows a cost by mistake. State 11 kW if the car has the standard one.

## What a choice changes

- **A charger** applies at once: costs are computed on each reading.
- **A variant** changes the energies, and with them consumptions and costs. The collector works out the vehicle's trips and charges again after its next reading (within 10 minutes while parked), keeping the entered costs.

## Its sheet

Below the model, the vehicle's settings show its **Sheet**: what the car's maker tells of it beyond its model, and only that. Nothing is shown for a value it does not tell.

- **A Volvo**: its colour, its drive, its power, its first registration and its seats; under **Show more**, its gearbox, its gross weight, its engine displacement (a plug-in hybrid's), its upholstery (most often Volvo's code) and the side of its steering wheel. And its picture, which also shows beside its name in the menu and on its state.
- **A Tesla**: the name you gave it in Tesla's app, its colour; under **Show more**, the side of its steering wheel, its wheels and roof as Tesla names them, a third row of seats and a performance package. The name is also what Runsten calls it, in the menu and in [Home Assistant](./mqtt.md).
- **A Polestar** read through the Data Portal tells none of it: no sheet.

Your browser never loads the picture from Volvo: Runsten fetches it from Volvo's servers, keeps it a day, and gives it to your browser itself.

## Net capacity

A car that reports its battery's capacity (a Volvo) does not say which: Volvo's API has given the gross capacity, a pack's label or the net capacity. Runsten assumes, until a measurement confirms it, that the state of charge runs over the net (usable) capacity, and uses it when the catalog knows it. For an EX30 of 69 kWh gross, 64 net, energies are 7 % lower than on the gross capacity.

## Plug-in hybrids

A plug-in hybrid is read like any other car of its brand. Runsten knows a car is one when its manufacturer's API says so: a Volvo's tells its fuel type. Polestar's Data Portal does not: it reads every car as an electric one, so a Polestar 1 is never recognized as a plug-in hybrid. Tesla makes none.

What works for a plug-in hybrid:

- **Charges on a plug**, with their energy and their cost. While driving, the engine may also raise the state of charge (holding an empty battery, or a charge mode): such a rise is never taken for a charge. A rise seen while parked is a charge only when the car says its cable is connected.
- **Trips**, with their distance and the energy taken from the battery.
- **The variant**, its net capacity and its onboard charger, as for an electric car.
- **The health** (tyres, service, warnings) and the current state.

What does not, yet:

- **Consumption**: none is shown, for a trip or in the statistics. The state of charge tells the battery's energy only, not which part of a trip the engine drove: an energy over the whole distance would pass for a very low consumption.
- **Fuel**: the fuel a trip burned, the refuels and their cost are not counted.

A missing or wrong variant? [Contribute it](../contributing/variants.md).
