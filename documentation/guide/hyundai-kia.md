---
description: "Read a Hyundai, Kia or Genesis with Runsten: the Pleos Vehicle Data API, the key you create in the Pleos Playground with the primary user's account, one key per brand, and the one reader a key serves."
---

# Your Hyundai, Kia or Genesis

Runsten reads a Hyundai, a Kia or a Genesis through **the Pleos Vehicle Data API**: the access to their cars that Hyundai, Kia and Genesis, through Pleos, open to their owners, as the EU Data Act gives them. In the Pleos Playground you create a **key of your own**, and give its Client ID and its Client secret to Runsten. Runsten never asks for the password of your Bluelink, Kia Connect or Genesis Connected account. A self-hosted instance needs nothing else from the makers.

::: warning Beta
Hyundai, Kia and Genesis support is in beta. It was built from Pleos's documentation, its examples and the free clients that already read the API (evcc, the Home Assistant integrations), against a simulator: no real car has been read yet. What Pleos does not publish is assumed until then — how long a token lives, how many calls a key may make, the units of what a car sends and how often it sends — and may differ from what Runsten expects. What the Connection page tells you, and the [troubleshooting](#when-it-is-not-read) below, are the first things to look at.

On an instance where Hyundai, Kia and Genesis are turned off ([Feature flags](../self-hosting/flags.md)), Runsten reads nothing of your Hyundais, Kias and Genesises, and takes no new key. The tab of each of these brands on the Connection page then shows only for an account that already has a key or a car of it: it says the brand is turned off, and a key already given stays shown, and can be removed.
:::

Runsten is not affiliated with Hyundai, Kia, Genesis or Pleos. It only reads: it never sends a command to the car.

## What the My Vehicle Data API is

The My Vehicle Data API serves the cars of the European markets only: the EU, the United Kingdom, Switzerland, Norway and the others of Pleos's list, as of the day you read this. A car needs its connected services active (Bluelink, Kia Connect, Genesis Connected), their data collection agreed and its ePrivacy set accordingly — in the car's own screens or the maker's app.

Two things decide what a key reads:

- a car you are the **primary user** of in the maker's account: only the primary user's key lists cars; a secondary or shared account's lists none;
- the cars **registered at the moment the key was issued**: a car added to the account later is not read, and to read it you must issue a new key.

**One key per brand**: a household with a Kia and a Hyundai gives two keys, each in the tab of its make.

## Create your key

You do this once per brand, on a computer, in the Pleos Playground. The Playground shows **My Vehicle Data API** only to users in Europe.

1. Sign up at the [Pleos Playground](https://developers.pleos.ai/) and open **Workspace**, then **My Vehicle Data API**.
2. Open the **tab of your car's brand** — Hyundai, Kia or Genesis. The brand is not guessed later: it is the tab you take the key from, and you give the key in the same brand's form in Runsten.
3. Tick the **two boxes**: Terms and Conditions, Privacy Policy.
4. Choose **Get API Key**. A pop-up asks you to sign in with your car's account — **Bluelink, Kia Connect or Genesis Connected** — **as its primary user**, not with a secondary or shared one. An e-mail confirms the key was issued.
5. On the key's page, copy the **Client ID** and the **Client secret**, with the page's copy buttons. The Playground also shows the key's expiry there: Pleos does not tell it through the API, so note it while you are on the page.

## Give it to Runsten

On the **Connection** page (in the menu under your name), in the tab of your car's brand (**Hyundai**, **Kia** or **Genesis**), paste the Client ID and the Client secret, then **Save the key**. Runsten checks the key first: it asks Pleos for a token and lists the cars the key reads.

- A key Pleos refuses is not saved: revoked, replaced or expired in the Playground. Check the key's page there.
- A key Pleos could not check, its servers failing or out of reach, is not saved either: try again in a moment.
- A key that lists no car is not saved either: Pleos accepted it, but it reads none of your cars. Saving the same key again changes nothing: see [when the key lists no car](#when-the-key-lists-no-car).
- A key whose cars would take your account past the number of vehicles it may have is not saved either.
- Otherwise the cars the key lists are added to your account, and read from the collector's next pass.

The key is stored encrypted and is only ever sent to Pleos. It is never shown again: the Connection page shows the last four characters of its Client ID, and when it was given.

## One key, one application

Pleos keeps **one live token per key**: asking for a new token ends the one before. Two applications that read with the same key — Runsten and evcc, or a Home Assistant integration — take each other's place in turn, and neither reads steadily. A token refused once is only asked for again. When Pleos refuses a token that should still live three times within an hour, Runsten takes it that another application reads with the key: the Connection page says **Key shared with another application** under the car's latest failure, Runsten leaves the key alone for a quarter of an hour, then asks for a new token and reads again.

To read the car and keep it in Home Assistant, give Runsten the key alone, and let Runsten **republish the car to your MQTT broker**: Home Assistant creates the car's entities by itself, with what Runsten read ([Home Assistant and MQTT](./mqtt.md)). Nothing else needs to read with your key.

## What is read, and how often

Runsten reads the battery level and its target (during a fast charge, the DC target set in the car; otherwise the AC one), whether the car charges — AC (the API calls it a slow charge), DC (fast) or a scheduled one — whether the cable is connected, the time left, the battery's capacity, the range, whether the car is on, the odometer, whether it sleeps, the position, the doors, the boot, the bonnet and the sunroof, the central lock, the tyre pressures (converted to kPa from the unit the car names, psi when it names none Runsten knows, and shown in bar as for the other cars; how Pleos scales its kPa and bar values is assumed until a real car confirms it, so check them against the car's own screen), the warnings (the car's [health](./health.md)), and whether the climate runs, with its setpoint. The car's model and its model year are read with its key: when one of Runsten's variants fits, its net battery capacity carries the energies and the model shows as for the other brands ([Vehicle models](./vehicle-models.md)). A charge has **no power**: the API gives no current, no voltage and no phases, so Runsten's charge shows its energy and its battery, nothing between. Not every car serves everything: some send no doors or no tyre pressures, and their pages show what came. The responses Runsten kept from Pleos are in the [readings' CSV file](./exporting.md), as Pleos sent them: a response identical to the previous one is not kept again, and of the list of the key's cars, only the car's own entry is.

- Parked, Runsten reads the battery and whether the car is on every 10 minutes, and the position, the range and the state of the car every hour. While the car drives or charges, it reads the battery, whether the car is on and the position every 2 minutes, the range every 10, the state of the car every hour. At the end of a trip or a charge, and at the start of a charge, whether the car is on, the position and the range are read at once. The model of the car is read once a day.
- The car sends at its own pace, and the API cannot wake it: reading more often than the car sends gives nothing more. A parked car may send hours-old values however recently Runsten read them: the vehicle's page tells when the car last measured them, once that is more than half an hour ago.
- Pleos publishes no quota of calls. Runsten makes **2,000 calls a day at most** per key, of its own: a car parked all day spends about 360, two hours of driving and four of charging add some 550, and the key is yours alone. With the calls used up, reading resumes by itself when the day turns.

## When the key ends

Pleos shows the key's validity on its page in the Playground; the API does not tell it, and Runsten does not remind you before it passes (a reminder will come once the real duration is known). A key that expires, like one revoked or replaced, is refused: the Connection page says **Pleos refused the key** under each of its cars, and you issue a new key and give it with **Replace the key**. Nothing already read is lost.

Note that **revoking or reissuing the key ends the access tokens** issued with the one before it: every application that read with the old key stops, and Runsten asks a token again with the new one by itself.

## When the key lists no car

Pleos accepted the key, but tells Runsten it reads no car: the key is not saved. The list of a key's cars is fixed when the key is issued, so saving the same key again gives the same answer; in each case below, the fix is a new key. The known causes, the most frequent first:

- **Not the primary user.** The key was issued with an account the car is only shared with — a partner's, a second driver's. In the Bluelink, Kia Connect or Genesis Connected app, check that this account is the car's **primary user**; if not, issue the key with the primary user's account, or have the car transferred to yours in the app first.
- **The car came after the key.** A car registered or activated in the maker's account after the key was issued is not in its list. Revoke the key in the Playground, issue a new one, and give it again.
- **The wrong brand's tab.** A key belongs to the tab of the Playground it was issued from: a Hyundai's key issued in the Kia tab lists no Hyundai. Issue the key from your car's brand's tab, and give it in the same brand's form in Runsten.
- **A car locked by the maker.** A former fleet, demo or company car can stay locked and list nothing, even for its primary user. Only the maker's support lifts that: ask it to release the car for the connected services of your account, then issue a new key.

The connected services must be active, and their data collection agreed, in the car or the maker's app ([what the API is](#what-the-my-vehicle-data-api-is)).

## When it is not read

The vehicle's pages warn when nothing new is read of it; the Connection page says why:

- **Pleos refused the key**: revoked, replaced or expired in the Playground. Give a new one in the brand's form: what was read stays.
- **No key given**: it was removed for the brand. Give another.
- **The car no longer shares its data with the key**: the sharing was turned off in the car's connected services, or the car was unregistered. Should the sharing come back, give the key again: the car is read again, without losing anything.
- **Paused by Pleos** or **daily calls to Pleos reached**: reading resumes by itself, when Pleos says (a quarter of an hour when it does not) for a pause, the next day for the calls.
- **Pleos out of reach**, or its servers failing: that is never taken for a refusal of the key, which stays as it is. Runsten reads again by itself at its next pass.
- **Key shared with another application**: see [above](#one-key-one-application).
- **Nothing for a day** on a car that was driven: look at the connected services and the ePrivacy settings in the car, and check that the car has had mobile coverage: the API says nothing more.

## Removing it

**Remove the key** erases what Runsten keeps of it, per brand. Your cars stay, with their history, and are no longer read until you give another key. The key stays in the Playground: revoke it there if you no longer use it, which also ends every token issued with it, for good.
