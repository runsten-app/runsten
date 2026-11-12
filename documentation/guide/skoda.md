---
description: "Read a Škoda with Runsten: the MyŠkoda Public API, the key you create in the MyŠkoda app and the VIN of each car, its expiry, Škoda's 20 reads an hour per car, and its personal use only."
---

# Your Škoda

Runsten reads a Škoda through **the MyŠkoda Public API**: the access to their cars that Škoda opens to its owners, documented and free. In the MyŠkoda app you create a **key of your own**, tick the cars it reads, and give it to Runsten with the **VIN** of each car. Runsten never asks for the password of your Škoda account. A self-hosted instance needs nothing else from Škoda.

::: warning Beta
Škoda support is in beta. It was built from Škoda's documentation and its API's contract (version 1.1.0), against a simulator: no real car has been read yet. What Škoda does not publish is assumed until then — how often a car sends its state, how long a trip is told as one, how a key's lifetime is chosen — and may differ from what Runsten expects. What the Connection page tells you, and the [troubleshooting](#when-it-is-not-read) below, are the first things to look at.

On an instance where Škoda is turned off ([Feature flags](../self-hosting/flags.md)), Runsten reads nothing of your Škodas, and takes no new key. The Škoda tab of the Connection page then shows only for an account that already has a key or a Škoda: it says Škoda is turned off, and a key already given stays shown, and can be removed.
:::

Runsten is not affiliated with Škoda Auto. It only reads: the API also offers to start a charge or the climate, and Runsten never sends such a command.

## Personal use only

Škoda's terms allow the MyŠkoda Public API for **personal use only**: it may not serve a commercial product or service. A Runsten you host for yourself, and read your own cars with, is that personal use. An instance that serves other people turns the source off with the flag `source.skoda` ([Feature flags](../self-hosting/flags.md)); Škoda points commercial uses to its Charging Data API instead.

## What the API reads

The API serves the Škodas with MyŠkoda's connected services: the Enyaq, the Enyaq Coupé and the Elroq, the electric ones Runsten recognizes. The data a car gives depends on the car: what it does not support, or could not send, is simply missing from its pages.

Two things decide what a key reads:

- the cars **ticked when the key was created** in the app: a car added later needs a new key, or the key updated in the app;
- the **VIN** of each car, which you give with the key: the API reads a car by its VIN and does not list the cars of a key.

A key reads up to **five cars**. Up to five keys may read the same car: one for Runsten, others for evcc or Home Assistant.

## Create your key

You do this once, on the phone with the MyŠkoda app (version 8.16 or later).

1. On the phone, open [go.skoda.eu/api-keys](https://go.skoda.eu/api-keys): it opens the keys' page of the MyŠkoda app. On a computer, Škoda's [documentation](https://public.api.connect.skoda-auto.cz/docs) shows a QR code that leads there.
2. Create a key, and **tick each car** it is to read.
3. Copy the key: the app shows it only once.
4. Note the **VIN** of each car: in the MyŠkoda app, or on the registration document (17 characters).

## Give it to Runsten

On the **Connection** page (in the menu under your name), in the **Škoda** tab, paste the key, give the VIN of each car — one a line —, then **Save the key**. Runsten checks the key first, on each VIN: it reads the car's name with it.

- A key Škoda refuses is not saved: deleted in the app, or mistyped. Copy it again.
- A key that has expired is not saved either: create a new one.
- A key that does not read one of the VINs is not saved either, and the page names the VIN: the car was not ticked when the key was created, or the VIN is mistyped.
- A key Škoda could not check, its servers failing or out of reach, is not saved either: try again in a moment.
- A key whose cars would take your account past the number of vehicles it may have is not saved either.
- Otherwise the cars are added to your account, and read from the collector's next pass.

The key is stored encrypted and is only ever sent to Škoda. It is never shown again: the Connection page shows its last four characters, when it was given, and **when it expires**. Replacing the key fills the VINs of your Škodas in.

## When the key expires

A MyŠkoda key expires; Škoda tells when on every response, and the Connection page shows it. From **two weeks before**, the Škoda tab and the menu under your name carry a dot, and the tab says to renew the key: create a new one in the MyŠkoda app and give it with **Replace the key**. Once it has expired, Škoda refuses it, the car is no longer read, and the Connection page says the key has expired. Nothing already read is lost.

## What is read, and how often

Runsten reads, in one call, the battery level, the range, whether the car charges — and how: AC or DC, its **charging power**, the time left, the charge's target — whether the cable is connected, the odometer, the position once parked, the doors as a whole, the boot, the bonnet, the side windows as a whole and the sunroof, the central lock, and whether the climate runs, with its setpoint. The car's model is told by its VIN and its name in the app ([Vehicle models](./vehicle-models.md)); the API tells no battery capacity, so choose your variant when several fit. The responses Runsten kept from Škoda are in the [readings' CSV file](./exporting.md), without the licence plate nor the address Škoda writes from the position.

The API has **no ignition and no speed**: a trip is told by the car's parking position, which reads *in motion* while it drives, and by the odometer. A short trip between two reads may be missed, or two trips close together joined: a real car will tell how well this holds.

- Škoda allows **20 calls an hour per car**, a limit it says is not final; every response tells what is left of it, and Runsten keeps to that. Parked, Runsten reads the car every 20 minutes; while it drives or charges, every 5 minutes; at the end of a trip or a charge, and at the start of a charge, at once. That leaves a margin.
- The quota is the **car's**, shared by every application that reads it, whatever their key: evcc or Home Assistant reading the same car leave Runsten fewer calls. When the hour's calls run out, Runsten waits for the next hour, by itself, and nothing is shown as an error. To keep the car in Home Assistant, you can let Runsten **republish the car to your MQTT broker** rather than have Home Assistant read it too ([Home Assistant and MQTT](./mqtt.md)).
- The car sends at its own pace, and the API cannot wake it: reading more often than the car sends gives nothing more. Each value keeps the time the car measured it.

## When it is not read

The vehicle's pages warn when nothing new is read of it; the Connection page says why:

- **Škoda refused the key**: expired, or deleted in the MyŠkoda app. Give a new one: what was read stays.
- **No MyŠkoda key given**: it was removed. Give another.
- **The key no longer covers this car**: the car was unticked from the key in the app. Create a key with the car ticked and give it: the car is read again, without losing anything.
- **Škoda out of reach**, or its servers failing: that is never taken for a refusal of the key, which stays as it is. Runsten reads again by itself; such a call still counts in Škoda's hour.
- **Nothing for a day** on a car that was driven: check the car's connected services in the MyŠkoda app, and that the car has had mobile coverage: the API says nothing more.

## Removing it

**Remove the key** erases what Runsten keeps of it. Your Škodas stay, with their history, and are no longer read until you give another key. The key stays in the MyŠkoda app: delete it there if you no longer use it.
