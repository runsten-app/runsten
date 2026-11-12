---
description: "Create your application on the Volvo Cars developer portal, choose its redirect URI and connect your Volvo ID to Runsten."
---

# Your Volvo application

Runsten reads a Volvo through the [Volvo Cars API](https://developer.volvocars.com/). Each self-hosted instance that reads a Volvo uses an application of its own on the Volvo Cars developer portal, as Home Assistant's [Volvo integration](https://www.home-assistant.io/integrations/volvo/) does with its own credentials: its client ID, client secret and API key go into `.env`.

The application is yours, for your own Volvo ID: Volvo has nothing to review or approve. Its **Publish** form only gives it its OAuth credentials, at once.

An instance that reads only Polestars needs none: leave the Volvo variables of `.env` empty, and the Connection page offers no Volvo.

Why publish at all: the access tokens of the portal's generation tool last 30 minutes at most and cannot be renewed. The collector runs for weeks; it renews its tokens itself, which Volvo allows only with the client ID and secret of a published application.

## Creating the application

Choose the **redirect URI** first: the settings of the application cannot be changed once it is published. It is the address you open Runsten at (`RUNSTEN_APP_URL`) followed by `/auth/volvo/callback`:

- behind a [reverse proxy](./reverse-proxy.md): `https://runsten.example.org/auth/volvo/callback`;
- under a path: `https://example.org/runsten/auth/volvo/callback`;
- [on the local network only](./reverse-proxy.md#on-the-local-network-only): `https://192.168.1.20/auth/volvo/callback`, reported to work with a private address;
- without a proxy, on the server itself or through an SSH tunnel: `http://127.0.0.1:8082/auth/volvo/callback`, if Volvo accepts it (not verified yet).

Then, on the [developer portal](https://developer.volvocars.com/):

1. Create an account, or sign in.
2. On the [API applications](https://developer.volvocars.com/account/#your-api-applications) page, create an **API application** with a name of your choice. Its **API key** shows at once.
3. Select **Publish** under the application, and fill in the required fields: expand and select every **scope**, and add your redirect URI.
4. Select **View summary**, then confirm. The confirmation page gives the **client ID** and the **client secret**. The application then shows as "Publication under review": it works for your own Volvo ID all the same, without waiting.

If a scope is missing from your application, set `RUNSTEN_VOLVO_SCOPES` to the list it has (`.env.example` shows the default).

The client ID of an application created since Volvo ID's move to a new server starts with `vid-` (`vid-dc-` today): Runsten then signs in through the new Volvo ID (`https://auth.volvocars.com/realms/volvocars`). An older one, starting with `dc-`, goes through the former Volvo ID until it stops working (see below).

In `.env`:

```sh
RUNSTEN_VOLVO_CLIENT_ID=…
RUNSTEN_VOLVO_CLIENT_SECRET=…
RUNSTEN_VOLVO_API_KEY=…
```

Runsten derives the redirect URI from `RUNSTEN_APP_URL`: it must be the one registered with the application. Set `RUNSTEN_VOLVO_REDIRECT_URI` only if you registered another, on the host your browser uses.

## Moving to the new Volvo ID

Volvo ID moves to a new server: the applications whose client ID starts with `dc-` stop working after January 31, 2027 ([Volvo's announcement](https://developer.volvocars.com/news/volvoid-migration/)). Before then:

1. Create a new application on the developer portal, as above, with the same redirect URI. Its client ID starts with `vid-dc-`, and it has an API key of its own.
2. Put its client ID, client secret and API key in `.env`, in place of the former ones, and restart Runsten (`docker compose up -d`). If your `.env` sets `RUNSTEN_VOLVO_AUTH_URL`, remove it, or set it to `https://auth.volvocars.com/realms/volvocars`: it wins over the client ID.
3. Reconnect your Volvo ID: the former application's tokens are refused by the new Volvo ID, so every vehicle page asks for it, with a **Reconnect the Volvo ID** button.

   On the new Volvo ID's sign-in page, tick **Remember me**: without it, the connection ends after an hour (see [Connecting your Volvo ID](#connecting-your-volvo-id)).
4. Once the vehicles are read again, delete the former application on the portal.

An account that gave an API key of its own on the Connection page gives the new application's there.

## The quota

Volvo grants an application 10,000 calls a day per API (Connected Vehicle, Energy, Location), counted on its API key: every vehicle read with the same key spends the same quota.

With the default intervals, a vehicle spends a few hundred calls a day parked, and up to about a thousand on a day of driving and charging. One application is then enough for the vehicles of a household. Shorter intervals (`RUNSTEN_POLL_*` in `.env`) or more vehicles spend more.

Runsten spreads 90 % of the quota over the day. Above it, the calls wait and the readings come less often; the Connection page tells when the quota is exhausted. Should your application be granted another quota, set it in `RUNSTEN_VOLVO_DAILY_QUOTA`.

## Connecting your Volvo ID

Open the web interface, sign in, then follow **Connect a Volvo ID** on the home page, and log in with your Volvo ID. The connection and its vehicles are recorded in your account; the tokens are stored encrypted.

On the new Volvo ID (an application whose client ID starts with `vid-`), tick **Remember me** on Volvo's sign-in page: without it, Volvo ends the connection one hour after the sign-in, however often Runsten renews it. Runsten reads the lifetime Volvo gives the connection: when it is that short, the vehicles are recorded all the same, and the Connection page asks you to connect again, ticking the box.

The collector then refreshes the tokens before they expire, and at least once a day. A Volvo grant lasts 6 months at most: when it ends, or if Volvo refuses a refresh, the collector stops polling and every page of the vehicle says so, with a **Reconnect the Volvo ID** button.

## The Connection page

The **Connection** page of the web interface tells when the Volvo ID was authorized and renewed, and for each vehicle what the collector did on its latest pass: its polling mode, its latest successful reading and the next one due, an exhausted quota, a pause, its latest failed call.

The pages of a vehicle warn when nothing new is read of it: the grant lost, a pause, a quota, or a collector that has not passed for 15 minutes.
