---
description: "Read a Tesla with Runsten: a refresh token of your Tesla account, as TeslaMate takes it, the Owner API, a car left to sleep, and what is still in beta."
---

# Your Tesla

Runsten reads a Tesla through the **Owner API**, the API of the Tesla app, with a **refresh token** of your Tesla account: the way TeslaMate does. You get the token with a tool that signs you in on Tesla's own page, and give it to Runsten. Runsten never asks for your Tesla password. A self-hosted instance needs no Volvo application to read a Tesla.

::: warning Beta
Tesla support is in beta. It was built against a simulator of the Owner API, from responses of a real Model Y, and how often it reads is still being tuned on a real car.

The Owner API is **not official**: Tesla calls it deprecated in favour of its Fleet API, which is paid and needs an application registered with Tesla. It still answers owners' tokens, but Tesla may close it without notice. Should Tesla close it to your account, the Connection page says so.
:::

On an instance where Tesla is turned off ([Feature flags](../self-hosting/flags.md)), the Connection page has no Tesla tab.

Runsten is not affiliated with Tesla. It only reads: it never sends a command to the car, and **never wakes it up**.

## Get your refresh token

1. Run [Tesla Auth](https://github.com/adriankumpf/tesla_auth/releases/latest), from the author of TeslaMate, version 0.13.0 or newer (macOS, Windows, Linux), or [Auth app for Tesla](https://apps.apple.com/app/auth-app-for-tesla/id1552058613) (iOS, macOS): TeslaMate points to the same two.
2. Sign in with your Tesla account. The page is Tesla's own: your password, and your second factor if you have one, stay between you and Tesla.
3. The tool shows two tokens: copy the **refresh token**, not the access token.

## Give it to Runsten

On the **Connection** page (in the menu under your name), under **Tesla**, paste the refresh token, then **Save the token**. Runsten checks it first: it asks Tesla for an access with it and lists your account's cars.

- A token Tesla refuses is not saved. Signing out of the Tesla account revokes it, and Tesla may accept a refresh token only once: get a new one.
- A token whose account has no car is not saved either.
- Otherwise the cars of the account are added to yours, and read from the collector's next pass.

In exchange, Tesla may give a new refresh token: Runsten keeps that one, as the one you pasted may no longer work. You do not need to give it again, unless Tesla refuses it. The token is stored encrypted and is only ever sent to Tesla. It is never shown again: the Connection page shows the last four characters of the one you gave, and when.

Should you also use TeslaMate, give each its own token: two applications refreshing the same one may push each other out.

## Letting the car sleep

A Tesla stays awake while it is read, and an awake car loses a few kWh a day. Runsten reads it so that it can fall asleep:

- Every minute, it asks Tesla whether the car is awake: that does not wake it.
- While the car is awake, Runsten reads its data every minute, every 15 seconds while it drives.
- Once the car has been parked, not charging, for 3 minutes, Runsten stops reading its data for 15 minutes. If it falls asleep, it is left alone until it wakes up; if not, the reading resumes, and stops again 3 minutes later.
- An asleep car is never woken: what it measured while asleep is read when it wakes up.

The vehicle's page says when the car is asleep, or out of reach (asleep or without a network: Tesla does not always tell which), and since when: its values are then those it had before.

A trip that starts while Runsten leaves the car alone is seen when it ends, or when the 15 minutes are over: it is then found from the odometer, as a reconstructed trip.

Runsten makes at most 3,000 calls a day per token, to stay clear of limits Tesla does not publish.

## The energy of its charges

A Tesla counts the energy that goes into its battery during a charge. Runsten reads that count: a charge shows the energy measured by the car beside its two estimates, and its cost rests on it rather than on the change of the state of charge. The charges recorded before Runsten kept it have none until the instance rebuilds its trips and charges ([Updating](../self-hosting/updating.md#update-notes)).

## Its battery's capacity

With that energy, the car's rated range and its usable charge, the [battery page](./battery.md#a-tesla) estimates the capacity of a Tesla's battery as TeslaMate does: the rated range at the end of each charge, brought back to 100 %, converted into kWh. Its estimate rests on the car's own count, far more precise than a Volvo's or a Polestar's; it is still an estimate, beside the usable capacity of the variant. The charges recorded before Runsten kept the rated range give none until the instance rebuilds its trips and charges ([Updating](../self-hosting/updating.md#update-notes)).

## Its software

The vehicle's page gives the version of the car's software, and the update waiting for it, when there is one: available, scheduled, downloading (with its progress), waiting for Wi-Fi, or installing. The **Health** tab lists the versions it had, newest first, each with when the car took it, between two readings of Runsten: tie a change of range or of consumption to an update with it, as with TeslaMate. A version installed before the instance read the car shows from its first reading.

## Its variant

The Owner API gives the car's model (Model 3, Model Y, Model S, Model X), its trim badge (`74d` for a Long Range Dual Motor, `p74d` for a Performance, `50` for a Standard Range or Rear-Wheel Drive), and Runsten reads its model year from the VIN; it gives no battery capacity: the vehicle's page shows the usable capacity of its variant instead. Runsten's catalogue has the European Model 3 and Model Y ([Vehicle models](./vehicle-models.md)):

- When one variant alone fits the model, the year and the badge, it is recognized: a Model Y Long Range Dual Motor of 2021, for one.
- Often several fit: Tesla changed packs within a year (LG or Panasonic cells, CATL or BYD), and the 2024 Model 3 and 2025 Model Y report badges not known yet. In the vehicle's settings (the menu of its name in the bar), choose yours: without a variant, the trips have no energy, and the charges only the one the car measured.
- The Model S and Model X, and the North American variants, are not in the catalogue yet.

## When it is not read

The vehicle's pages warn when nothing new is read of it; the Connection page says why:

- **Tesla refused the refresh token**: you signed out of the Tesla account, or changed its password. Get a new token and give it.
- **Tesla refused the refresh token**, the latest failure being **API closed to the account**: Tesla closed its Owner API to your account. A new token does not change that: Runsten cannot read the car until Tesla opens it again.
- **No Tesla refresh token given**: it was removed. Give a new one.
- **The vehicle is no longer in the Tesla account**: sold, or removed from it. Should it come back, give the token again.
- **Paused by Tesla** or **Daily calls to Tesla reached**: reading resumes by itself.

## Removing the token

**Remove the token** erases it from Runsten. Your Tesla cars stay, with their history, and are no longer read until you give another. To revoke it at Tesla too, sign out of your Tesla account on every device from the Tesla app or tesla.com.
