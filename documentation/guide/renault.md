---
description: "Read a Renault, a Dacia or an Alpine with Runsten: the My Renault app's unofficial API, your account's email and password, the one-time code, Renault's quota, and what the cars tell."
---

# Your Renault, Dacia or Alpine

Runsten reads a Renault, a Dacia or an Alpine through **the API of the My Renault app**: the one the app itself uses. You sign in with the email and the password of your account. The same account serves MyDacia and MyAlpine. Runsten reads the electric cars of the account and adds them.

::: warning Beta, and unofficial
Renault, Dacia and Alpine support is in beta. It was built from the open source library [renault-api](https://github.com/hacf-fr/renault-api) (MIT), which the official Home Assistant integration for Renault uses, and from its recorded responses, against a simulator: no real car has been read yet. What Renault does not publish is assumed until then — how long a sign-in lasts, the quota, how often a car sends its state — and may differ from what Runsten expects.

This API is **not documented nor offered by Renault**, and its use goes against the app's terms. Renault may change it, or block an account, without notice. Runsten only reads: it never sends a command to the car, and never asks it to wake up.

The source is **off by default**. A self-hosted instance turns it on with the flag `source.renault` ([Feature flags](../self-hosting/flags.md)). Turn it on for your own instance, read by you: many accounts read from one address could get it blocked. Until it is on, the Renault tab of the Connection page shows only for an account that already has a Renault connection or car.
:::

Runsten is not affiliated with Renault, Dacia or Alpine.

## What the API reads

The API serves the cars with the app's connected services. What a car gives depends on its model: an endpoint a model does not serve is left out, and its values are simply missing from the car's pages. Runsten reads the **battery level and range, whether the car is plugged and charging, the time left, the odometer, the position, and whether the climate runs**, with the temperatures outside and inside.

Two limits to know:

- **No real charging power**, except on the first Zoe (phase 1). Elsewhere the figure the API calls charging power is what the car could accept, not what it receives. Runsten measures a charge by the battery levels at its start and end, on the capacity of the variant you choose, as it does for the other brands that tell no power.
- **No ignition and no speed**: a trip is told by the odometer and the position, read every few minutes. Trips are **approximate**, to about ten minutes. A short trip between two reads may be missed, or two close trips joined.

The cars covered are the electric ones Runsten recognizes: the Renault 5 and 4 E-Tech, the Mégane E-Tech, the Scénic E-Tech, the Zoe (both phases), the Twingo E-Tech, the Dacia Spring and the Alpine A290 ([Vehicle models](./vehicle-models.md)). The plug-in hybrids and the combustion cars of the account are ignored: the Connection page tells how many. The responses Runsten kept are in the [readings' CSV file](./exporting.md), without the registration number, the radio code nor the lists of the car's assets.

## Give it to Runsten

On the **Connection** page (in the menu under your name), in the **Renault** tab, give the **email and the password** of your My Renault account, then sign in.

- Some accounts must confirm with a **one-time code**. Renault emails it, the page says to which address (masked) and asks for the six digits. Give them within ten minutes. A wrong code leaves the page waiting for another.
- A wrong email or password is not saved ([When it is not read](#when-it-is-not-read) tells what else the message can mean).
- An account with no electric car is not saved either.
- Otherwise the electric cars of the account are added to your account, up to the number of vehicles it may have, and read from the collector's next pass.

**The password is never stored**, not even encrypted: it goes to Renault once, and Runsten keeps only the session Renault returns, encrypted. That session is only ever sent to Renault. Nothing of it is shown again.

## What is read, and how often

- The **battery** every 10 minutes while the car is parked, every 5 while it charges.
- The **position and the climate** every 30 minutes.
- The odometer every 10 minutes, the car's details (model, picture) once a day.

Reading does not wake the car: Renault returns the last values it received, each dated by the car. Reading more often than the car sends gives nothing more.

Renault does not publish its **quota**. Home Assistant's integration keeps to about **60 calls an hour per account**, and Runsten keeps below that. The quota is the account's, **shared with every other client** that reads it, such as Home Assistant: they leave Runsten fewer calls. When Renault says the quota is spent, Runsten pauses the account for **15 minutes**, by itself. To keep the car in Home Assistant, you can let Runsten republish it to your MQTT broker rather than have Home Assistant read it too ([Home Assistant and MQTT](./mqtt.md)).

## When it is not read

The vehicle's pages warn when nothing new is read of it; the Connection page says why:

- **Renault refused the sign-in**: the session Renault gave is no longer valid. Sign in again with your email and password: what was read stays.
- **Renault refused the email or the password**: mistyped, or the account asks for a code the sign-in cannot reach. If you are sure of them, Renault may have changed the key of its app, which fails with the same message. The instance's owner can set the new key in `.env`, as renault-api publishes it: `RUNSTEN_RENAULT_GIGYA_KEY` and `RUNSTEN_RENAULT_KAMEREON_KEY` (and `RUNSTEN_RENAULT_GIGYA_URL`, `RUNSTEN_RENAULT_KAMEREON_URL` should the hosts change), for `runsten-api` and `runsten-collector`, then restart them ([Feature flags](../self-hosting/flags.md)). The defaults are renault-api's, for Europe.
- **Privacy mode is on in the car**: the car shares nothing while it is on. Turn it off in the car's settings.
- **The car does not serve a value**, its position for instance: some models serve less. Nothing to do.
- **The quota is spent**, or **Renault out of reach**: Runsten reads again by itself.
- **No longer shared**: the car left the account, or the account changed on Renault's side. Sign in again.

## Removing it

**Remove** the connection erases what Runsten keeps of it. Your cars stay, with their history, and are no longer read until you sign in again.

## Credits

Everything above rests on the work of [renault-api](https://github.com/hacf-fr/renault-api) (MIT) and on the Home Assistant integration built on it: the hosts, the sign-in, the endpoints each model serves and their quirks come from there.
