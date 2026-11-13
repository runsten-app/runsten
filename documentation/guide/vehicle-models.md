---
description: "How Runsten recognizes the variant of your car, choosing it, the onboard charger, its specifications and the net battery capacity."
---

# Vehicle models

Runsten carries a catalog of the variants of the brands it reads: the electric Volvos and Polestars, the European Tesla Model 3 and Model Y, the electric BMWs and MINIs from the i3 on, the electric Hyundais, Kias and Genesis, the Škoda Enyaq, Enyaq Coupé and Elroq, and the plug-in hybrids (the T6 and T8 Volvos of the 60 and 90 series, the T4 and T5 of the XC40, the Polestar 1, and the BMWs and MINIs from 2014 on: the 225xe and 225e, 230e, 320e, 330e, 530e, 550e, M5, 740e, 745e, 750e, i8, X1, X2, X3, X5, XM and the Countryman): for each, its model years, its gross and net battery capacities, its onboard charger (standard, and the optional 22 kW one), its DC power (most plug-in hybrids have none), and the sources of every figure. It comes with Runsten: an update brings its corrections.

The model matters for two things: the net capacity, on which energies rest, and the onboard charger, which bounds the cost of an AC charge.

## Recognition

The variant is worked out from what the manufacturer's API reports of the car and its VIN; nothing more is collected. What that is depends on the brand, below. The vehicle is recognized when exactly one variant fits, among those of its powertrain: a plug-in hybrid among the plug-in hybrids, an electric car among the electric ones, so that a hybrid is never taken for an electric car of a battery nearby. A car whose fuel type is not read yet, or that runs on fuel alone, has no variant.

When several variants fit, or none, the vehicle has no variant until you choose one: its energies then rest on the capacity the car reports, if it reports one (a Volvo, a BMW, a MINI, a Hyundai, a Kia or a Genesis does; a Polestar, a Tesla or a Škoda does not).

A Volvo tells its model, its model year, its fuel type and a capacity of its battery: it is recognized from them.

A Polestar read through the Data Portal tells its model year (from its VIN), but neither its model nor its battery's capacity: several Polestar variants fit, and none is recognized. Choose yours: without a variant, its energies are unknown ([Your Polestar](./polestar.md#choose-its-variant)).

A Tesla tells its model, its model year (from its VIN) and its trim badge, but not its battery's capacity: it is recognized when exactly one variant fits its model, year and badge, a variant that lists no badge always fitting. Otherwise, choose yours among those that fit: until you do, its trips and charges have no energy ([Your Tesla](./tesla.md#its-variant)).

A BMW or a MINI, read through BMW CarData, tells its model's name (i4 eDrive40: its family, i4, and its trim, eDrive40), its powertrain, its date of construction and a capacity of its battery: it is recognized from its family, its model year, that capacity and its trim. Two readings are assumed until a real car confirms them: the model year is the year of construction, or the next one for a car built from July on, when BMW's model year changes; and the capacity is the gross one ([Your BMW or Mini](./bmw.md)). Others are assumed too, until a real car's model name confirms them: a plug-in hybrid's family is its model's number for the Series (a 330e's is 330e, an X5 xDrive50e's X5), a Countryman plug-in hybrid's is Countryman, a John Cooper Works' is its model's (Cooper, Aceman), and an i3 with its range extender reads as an electric i3. When the model year or the capacity is read otherwise, choose the variant in the vehicle's settings; when the family or the powertrain is, its variant is not among those offered, if any are: [tell us](../contributing/variants.md).

A Hyundai, a Kia or a Genesis, read through Pleos, tells its sales name (its family: IONIQ 5, EV3), its powertrain, its model year (from its VIN) and a capacity of its battery: it is recognized from them. How Pleos counts that capacity is assumed until a real car confirms it ([Your Hyundai, Kia or Genesis](./hyundai-kia.md)).

A Škoda, read through the MyŠkoda Public API, tells its model year (from its VIN) and, unless its owner renamed it in the app, its model's name; its family otherwise comes from its VIN (an assumption until real cars confirm it). It tells no capacity of its battery: an Enyaq, an Enyaq Coupé or an Elroq is recognized only when one variant alone fits its model and year, which is rare. Choose yours: until you do, its trips and charges have no energy ([Your Škoda](./skoda.md)).

## Choosing

In the vehicle's settings (the menu of its name in the bar), choose the variant among those of its family (of its brand, for a Polestar of the Data Portal: the variant chosen sets its model), the candidates of the recognition first. Each shows its capacities and powers, and whether they come from the manufacturer or from public sources.

The choice outranks the recognition; **Automatic recognition** goes back to it.

## Onboard charger

When the variant offers a 22 kW onboard charger as an option, state which one the car has. Unstated, 22 kW is assumed: a higher bound never narrows a cost by mistake. State 11 kW if the car has the standard one.

## What a choice changes

- **A charger** applies at once: costs are computed on each reading.
- **A variant** changes the energies, and with them consumptions and costs. The collector works out the vehicle's trips and charges again after its next reading (within 10 minutes while parked), keeping the entered costs.

## Its specifications

Below the model, the vehicle's settings show its **Specifications**: what the car's maker tells of it beyond its model, and only that. Nothing is shown for a value it does not tell.

- **A Volvo**: its colour, its drive, its power, its first registration and its seats; under **Show more**, its gearbox, its gross weight, its engine displacement (a plug-in hybrid's), its upholstery (most often Volvo's code) and the side of its steering wheel. And its picture, which also shows beside its name in the menu and on its state.
- **A Tesla**: the name you gave it in Tesla's app, its colour; under **Show more**, the side of its steering wheel, its wheels and roof as Tesla names them, a third row of seats and a performance package. The name is also what Runsten calls it, in the menu and in [Home Assistant](./mqtt.md). And its picture, as Tesla's configurator draws a Model S, 3, X or Y in its colour and on its wheels: the model as it was first sold (a 2025 Model Y looks like the one before it), and the model's first wheels when Tesla's are not among those it draws. A colour it does not draw for the model: no picture rather than a car of another colour. The administrator of the instance may turn it off ([Feature flags](../self-hosting/flags.md)).
- **A BMW or a MINI**: its colour, as BMW names it in English in every language (Brooklyn Grey), or BMW's code (B71) for a paint Runsten does not list, such as a paint made to order; under **Show more**, the side of its steering wheel. And its picture, as for a Volvo.
- **A Polestar** read through the Data Portal tells none of it: no specifications.

Your browser never loads the picture from Volvo or Tesla: Runsten fetches it from their servers, keeps it a day, and gives it to your browser itself. A BMW's is read once a day with the access you gave, kept at 640 pixels wide at most, and given the same way.

## Net capacity

A car that reports its battery's capacity (a Volvo) does not say which: Volvo's API has given the gross capacity, a pack's label or the net capacity. Runsten assumes, until a measurement confirms it, that the state of charge runs over the net (usable) capacity, and uses it when the catalog knows it. For an EX30 of 69 kWh gross, 64 net, energies are 7 % lower than on the gross capacity.

## Plug-in hybrids

A plug-in hybrid is read like any other car of its brand. Runsten knows a car is one when its manufacturer's API says so: a Volvo's tells its fuel type, a BMW's or a MINI's its drive train. Polestar's Data Portal does not: it reads every car as an electric one, so a Polestar 1 is never recognized as a plug-in hybrid. Tesla makes none.

What works for a plug-in hybrid:

- **Charges on a plug**, with their energy and their cost. While driving, the engine may also raise the state of charge (holding an empty battery, or a charge mode): such a rise is never taken for a charge. A rise seen while parked is a charge only when the car says its cable is connected.
- **Trips**, with their distance and the energy taken from the battery.
- **The variant**, its net capacity and its onboard charger, as for an electric car.
- **The health** (tyres, service, warnings) and the current state.

What does not, yet:

- **Consumption**: none is shown, for a trip or in the statistics. The state of charge tells the battery's energy only, not which part of a trip the engine drove: an energy over the whole distance would pass for a very low consumption.
- **Fuel**: the fuel a trip burned, the refuels and their cost are not counted.

A missing or wrong variant? [Contribute it](../contributing/variants.md).
