---
description: "Read a BMW or a Mini with Runsten: BMW CarData, a client ID you create in the MyBMW or MyMINI portal, the code you approve on BMW's page, what is read, and the stream only one application can hold."
---

# Your BMW or Mini

Runsten reads a BMW or a Mini through **BMW CarData**, the official service BMW opens to the owner of a car, free of charge. You create a **client ID** of your own in the MyBMW or MyMINI portal, give it to Runsten, then approve a code on BMW's own page. Runsten never asks for your BMW password, and a self-hosted instance needs nothing else from BMW.

::: warning Beta
BMW support is in beta. It was built from BMW's documentation, its API files and the free clients that already read CarData (the Home Assistant integrations), against a simulator: it still has to be checked on more real cars, and the names and units of what a car sends may differ from what Runsten expects. What the Connection page tells you, and the [troubleshooting](#when-it-is-not-read) below, are the first things to look at.

On an instance where BMW is turned off ([Feature flags](../self-hosting/flags.md)), the Connection page tells so, and reads nothing of your BMWs and Minis.
:::

Runsten is not affiliated with BMW or MINI. It only reads: it never sends a command to the car.

## What CarData is

CarData serves the BMWs and, in the EU, the Minis of 32 markets (the EU, the United Kingdom, Switzerland, Norway, Liechtenstein and Montenegro). It needs:

- a car you are the **main user** of in your BMW or MINI account: only the main user can read it;
- an active **ConnectedDrive** contract for the car.

CarData has two ways to read a car, and Runsten uses both:

- a **stream**: the car pushes what changes, each value dated, with no limit of calls;
- an **API**, limited by BMW to **50 calls a day**: it gives the latest value of each key.

## Create your client ID

You do this once, on a computer, in the portal of your country's BMW or MINI website. The portal has pitfalls: follow the order.

1. Sign in to the **MyBMW** (or **MyMINI**) portal, open the **BMW CarData** tile and choose **Create CarData Client**.
2. **Note the client ID right away**: the portal does not make it easy to find again. It is a long string of letters, digits and dashes.
3. Subscribe the client to **CarData API** and to **CarData Streaming**, **one at a time**: wait until the first is confirmed before the second. Runsten needs both: without the first it cannot read, without the second it receives nothing.
4. **Then** configure the stream (CarData Streaming's data selection): tick the keys Runsten reads, below. A key you do not tick is never sent, and the car then looks silent.
5. **Never click "Authenticate device"** in the portal: Runsten does it, with the code you will approve on BMW's page. Doing it in the portal does not connect Runsten.

The keys to tick, under the groups of the portal's catalogue:

| What | Keys (as BMW names them) |
|---|---|
| Ignition and movement | `vehicle.drivetrain.engine.isIgnitionOn`, `vehicle.isMoving` |
| Battery and range | `vehicle.drivetrain.batteryManagement.header`, `vehicle.powertrain.electric.battery.stateOfCharge.displayed`, `vehicle.powertrain.electric.battery.stateOfCharge.target`, `vehicle.drivetrain.electricEngine.kombiRemainingElectricRange` |
| Charging | `vehicle.drivetrain.electricEngine.charging.status`, `vehicle.drivetrain.electricEngine.charging.hvStatus`, `vehicle.drivetrain.electricEngine.charging.method`, `vehicle.drivetrain.electricEngine.charging.acAmpere`, `vehicle.drivetrain.electricEngine.charging.acVoltage`, `vehicle.drivetrain.electricEngine.charging.phaseNumber`, `vehicle.drivetrain.electricEngine.charging.timeToFullyCharged`, `vehicle.powertrain.electric.battery.charging.power`, `vehicle.body.chargingPort.status`, `vehicle.body.chargingPort.dcStatus` |
| Odometer | `vehicle.vehicle.travelledDistance` |
| Position | `vehicle.cabin.infotainment.navigation.currentLocation.latitude`, `vehicle.cabin.infotainment.navigation.currentLocation.longitude` |
| Tyres | `vehicle.chassis.axle.row1.wheel.left.tire.pressure`, `vehicle.chassis.axle.row1.wheel.right.tire.pressure`, `vehicle.chassis.axle.row2.wheel.left.tire.pressure`, `vehicle.chassis.axle.row2.wheel.right.tire.pressure` |
| Doors and locks | `vehicle.cabin.door.row1.driver.isOpen`, `vehicle.cabin.door.row1.passenger.isOpen`, `vehicle.cabin.door.row2.driver.isOpen`, `vehicle.cabin.door.row2.passenger.isOpen`, `vehicle.body.hood.isOpen`, `vehicle.body.trunk.isOpen`, `vehicle.cabin.door.lock.status` |
| Windows | `vehicle.cabin.window.row1.driver.status`, `vehicle.cabin.window.row1.passenger.status`, `vehicle.cabin.window.row2.driver.status`, `vehicle.cabin.window.row2.passenger.status`, `vehicle.cabin.sunroof.status` |
| Climate | `vehicle.vehicle.preConditioning.activity` |

Not every car sends every key. Some send neither the ignition nor the movement, even through the API (seen on an i4): Runsten then finds each trip from the odometer the car sends on the way, as a span of time, without the moment it set off.

::: tip Connected before Runsten read the doors
If you connected your BMW before Runsten read its doors, windows, locks and climate, tick the keys of the last three rows in the portal: the stream sends them from then on. Nothing else to do: Runsten replaces the set of keys it reads through the API by itself, once, at its next hourly read.
:::

## Connect it in Runsten

On the **Connection** page (in the menu under your name), in the **BMW** tab, under **BMW CarData**:

1. Paste the client ID, then **Get a code**. Runsten asks BMW for a code with it: a client ID BMW does not know is refused there and then.
2. Runsten shows the **code** and a link to **BMW's page**, valid for the time it tells (a few minutes). The link opens in a new tab: sign in there with your BMW or MINI account and approve the code. Your password stays between you and BMW.
3. Back in Runsten's tab, leave the page open: Runsten asks BMW every few seconds whether you approved, and connects by itself. **Cancel** stops the wait.

When BMW accepts, Runsten lists the cars of your account: the ones you are the main user of are added to yours, within the cars your account may have, and read from the collector's next pass. A BMW account that is the main user of no car, or whose client ID lacks the API subscription, is not connected: the page says which.

Runsten keeps the tokens BMW gives, encrypted, and only ever sends them to BMW. Nothing of the client ID is shown again but its last four characters, to find it in the portal, and when you connected.

## What is read, and how often

Runsten reads the battery level and the target, the range, the charging (status, cable and type, power, current, voltage, phases, time left), the odometer, the position, the tyres' pressure, the doors, the hood, the boot, the windows, the sunroof and the central lock, and whether the car is preconditioning, and the model of the car with its battery capacity, which tell its variant when one fits ([Vehicle models](./vehicle-models.md)). BMW's capacity is taken as the gross one, before the reserve, which is not yet checked on a real car: when no variant fits, it is the capacity the energies are counted on, and they may come out a little high if it is not. Its colour, by BMW's English name for its paint (Brooklyn Grey, Nanuq White) or else by BMW's code (B71) for a paint Runsten does not know yet, and its steering side show in its settings, and BMW's picture of the car beside it in the menu and on its state.

- The **stream** is read at the collector's pace: every minute while the car is parked, every 15 seconds while it drives or charges (at best: a collector busy with many vehicles, or an instance's own policy, may read it less often). It costs no call. A parked car that sleeps sends nothing: Runsten dates every value by the moment the car measured it, never by the moment it arrived.
- The **API** is the safety net: a value the stream did not push, and what a restart of Runsten lost, aimed at once an hour; the model of the car, its picture and its charging history, once a day. That is about 27 calls a day per car. Runsten makes **40 calls a day at most** per connection, of the 50 BMW allows, to leave you a few of your own: with two cars or more, the calls are spread over the day and the hourly reads come less often. The stream is not affected.

The doors, windows and lock show on the car's state and its Health page, and a car left parked unlocked or open is told, in Runsten and to [Home Assistant](./mqtt.md), as for the other brands. The users of the Home Assistant CarData integrations report these keys as **rare or unreliable on some cars**: the car may send them only now and then, or not at all. Runsten shows what it received, dated, and never takes a key it did not receive for closed. BMW tells which door is the driver's, and Runsten takes it for the front left one, as BMW's catalogue does: on a right-hand drive car, the left and right doors and windows may be swapped.

CarData has no key for the outside or cabin temperature, nor for the car's software version: Runsten cannot show them for a BMW or a Mini.

A trip is worked out from the ignition, the odometer and the position, as BMW gives no trip of its own. When the car pushes its position often enough while it drives, the trip has a route; Runsten does not promise it before it has been seen on real cars.

### Charging history

BMW keeps the charging sessions of the last 30 days, with the **energy each one drew from the grid**, losses included: a measurement that no Volvo, Polestar or Tesla gives. Runsten reads them once a day and gives each charge the energy of the sessions that took place within it. The charge then shows the energy drawn from the grid and the **losses**, its difference with the energy the battery took (itself measured or estimated), and its cost rests on that energy as it is, with no assumed efficiency ([Places, tariffs and costs](./costs.md)). A charge has it the day after it ends at the latest. How BMW dates and counts its sessions is not yet checked on a real car.

**The month before you connected** comes along: at its first read of the history, part of the first day's calls, Runsten imports the sessions that ended before it first read the car as charges, marked as imported from BMW CarData, with their times, their state of charge at both ends, the energy drawn from the grid, the energy the battery took (estimated from the state of charge), and where they took place. BMW tells neither AC nor DC, nor what a session cost: its cost is the tariff of its place, on the grid's energy. A session still running when you connected is left to the charge Runsten reads from then on, and those after it are Runsten's own charges: none is counted twice. The import shows in the car's settings, under **Imported history**, where you can delete it; it does not come back ([Importing your history](./importing.md#bmw-and-mini)).

## One application on the stream

BMW allows **one connection at a time to the stream**, per BMW account. **Runsten and another application cannot both read it**: the Home Assistant CarData integration, evcc or any other client of your BMW account. While another holds it, the Connection page says **Stream held by another application** under the car's latest failure, and Runsten reads nothing of the stream: it only has the API's hourly values.

To use both, let go of the stream in the other application, and let Runsten **republish the car to your MQTT broker**: Home Assistant then creates the car's entities by itself, with what Runsten read, and nothing else needs to hold BMW's stream. Set it up in **Settings, Integrations** ([Home Assistant and MQTT](./mqtt.md)).

## When it is not read

The vehicle's pages warn when nothing new is read of it; the Connection page says why:

- **BMW refused the connection**: BMW ended Runsten's access. A connection left unused for two weeks, or a client ID whose subscription was withdrawn in the portal, is refused. Select **Connect again**: what was read stays.
- **No BMW connected**: the connection was removed. Connect again.
- **The BMW account is no longer the car's main user**: the car was sold, or its main user changed. Should it come back, connect again.
- **Paused by BMW** or **daily calls to BMW reached**: reading resumes by itself. The stream, which has no limit, is read meanwhile.
- **Stream held by another application**: see [above](#one-application-on-the-stream).
- **Nothing received for a day** on a car that was driven: the car's page tells when the car last measured what it shows. Look in the portal, at the keys of CarData Streaming: a key that is not ticked is never sent. Then check that the car's ConnectedDrive contract is active and that the car has had mobile coverage.

When you connect, a message may tell why nothing was connected:

| Message | What to do |
|---|---|
| BMW does not know this client ID | Check the client ID in the portal, and that you copied all of it. |
| The code expired, or none was waiting | Get a new code, and approve it within the time shown. |
| BMW refused the connection | The code was declined on BMW's page, or BMW did not accept it: get a new code. |
| The client ID is not subscribed to CarData API | In the portal, subscribe the client to CarData API (and CarData Streaming), then get a new code. |
| This BMW account is the main user of no vehicle | Sign in on BMW's page with the account that is the car's main user. |
| Your account would have more vehicles than it may | The cars of this BMW account would exceed your account's limit: nothing is connected. |
| BMW did not answer | Try again in a moment. While you wait for the approval, Runsten keeps asking: the code may well be approved. |
| Too many connections to BMW are pending on this instance | Try again in a few minutes. |
| BMW is not offered on this instance | The administrator turned `source.bmw` off ([Feature flags](../self-hosting/flags.md)). |

## Removing it

**Remove the connection** erases the tokens Runsten keeps. Your cars stay, with their history, and are no longer read until you connect again. The client ID stays in the portal: delete it there if you no longer use it, which also ends BMW's access for good.
