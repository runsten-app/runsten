---
description: "Read a Polestar outside the EU and the EEA with Runsten: the Polestar app's API, your Polestar ID, on a self-hosted instance, and what it reads."
---

# Your Polestar, with the Polestar ID

Runsten reads a Polestar through [Polestar's Data Portal](./polestar.md), which is only open to the cars of the EU and the EEA: elsewhere (Switzerland, the United Kingdom, the United States…) it refuses them. For those cars, Runsten can read the **API of the Polestar app** instead, with your **Polestar ID**, the account you sign in to the app with.

::: warning Beta, self-hosted only
The Polestar app's API is **not official**: Polestar may change or close it without notice, and your Polestar account carries the risk. It is off by default: the administrator of your instance turns it on ([below](#turn-it-on)). Turn it on for your own instance only: many accounts read from the same addresses could get them blocked.

It was built against a simulator, from the behaviour of the Home Assistant integration `polestar_api`; the sign-in was tried on a real Polestar ID, the readings are still to be checked on a real car.
:::

A Polestar the Data Portal reads should stay with it: it reads more (the position, the doors and windows, the tyres).

Runsten is not affiliated with Polestar. It only reads: it never sends a command to the car.

## What it reads

The battery level, the range, the charging status, the odometer, and the service (the days and the distance to it, and the warnings of the brake fluid, the coolant and the oil). The model and its model year come with the car: Runsten recognizes its variant when one fits.

It reads **neither the position, nor whether the car drives, nor the charging power, nor whether the cable is connected**:

- A trip is found from the odometer, as a trip Runsten did not see: it has no start or end place, no route, and its times are bounds.
- A charge is found from the battery level and the charging status: it has no place, so no tariff of a place prices it; enter its cost if you want it counted ([Costs](./costs.md)). Its energy is the change of the battery level times the variant's capacity.

Runsten reads the car every 5 minutes, every minute while it charges, and makes at most 2,000 calls a day per Polestar ID, to stay clear of limits Polestar does not publish.

## Turn it on

On a self-hosted instance, in `.env`:

```sh
RUNSTEN_FLAGS=source.polestar-id=true
```

then restart the stack (`docker compose up -d`). See [Feature flags](../self-hosting/flags.md). The **Connection** page then shows, in the Polestar tab, a **Polestar ID** section under the Data Portal's.

## Connect your Polestar ID

On the **Connection** page (in the menu under your name), in the **Polestar** tab, under **Polestar ID**, choose how Runsten connects.

### Sign in on Polestar's page (recommended)

Runsten keeps no password: only the refresh token the sign-in gives. Polestar's sign-in only sends back to polestar.com, not to your instance: you bring the code back by hand.

1. On a computer, open your browser's developer tools (F12, or right-click and **Inspect**) on the **Network** tab, and tick **Preserve log**.
2. In Runsten, **Get the sign-in link**, then open **Polestar's sign-in page**, in the same window as the developer tools. The link is valid 10 minutes.
3. Sign in with your Polestar ID. polestar.com then shows an error page: that is expected, the code is Runsten's, not polestar.com's.
4. In the network log, find the request to `sign-in-callback` (filter on it), and copy its whole address: `https://www.polestar.com/sign-in-callback?code=…&state=…`.
5. Paste it in Runsten, then **Connect**.

A code is used once: should Runsten refuse it, get a new link and sign in again.

### Email and password

Runsten signs in as the Polestar app does, and **keeps your email and password**, encrypted, to sign in again when Polestar ends the session. Polestar's terms forbid giving your password to a third party: give it only to an instance you run yourself.

### Then

Either way, Runsten checks the connection first: it signs in and lists the cars of your Polestar ID.

- A sign-in Polestar refuses is not saved.
- A Polestar ID without a car is not saved either.
- Otherwise its cars are added to your account, and read from the collector's next pass. A car the Data Portal already reads for your account stays with it.

The Polestar ID gives a new refresh token at every refresh, every 5 minutes: Runsten keeps each one, encrypted, and only ever sends it to Polestar. Nothing of it is shown again: the Connection page shows how you connected, and when.

## When it is not read

The vehicle's pages warn when nothing new is read of it; the Connection page says why:

- **The Polestar ID refused the sign-in**: the session ended (signed out everywhere, or left unused too long), or the password changed. In the password mode, Runsten signs in again by itself first; when that fails too, connect again.
- **No Polestar ID connected**: it was removed. Connect it again.
- **The car is no longer in the Polestar ID**: sold, or removed from the account. Should it come back, connect again.
- **Paused by Polestar** or **Polestar quota reached**: reading resumes by itself.

On an instance where the Polestar ID is turned off again, its cars are no longer read; what was read stays, and they are read again once it is back on.

## Removing it

**Remove the Polestar ID** erases the refresh token, and the email and password with it. Your cars stay, with their history, and are no longer read until you connect again. To end the session at Polestar too, sign out of your Polestar ID everywhere, or change its password.
