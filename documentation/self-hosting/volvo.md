---
description: "Create your application on the Volvo Cars developer portal, choose its redirect URI and connect your Volvo ID to Runsten."
---

# Your Volvo application

Runsten reads the car through the [Volvo Cars API](https://developer.volvocars.com/). Each self-hosted instance uses its own application, created and published on the Volvo Cars developer portal: its client ID, client secret and API key go into `.env`.

## Creating the application

The settings of a published application cannot be changed, so choose the **redirect URI** first. It must lead to `/auth/volvo/callback` on the host where you sign in to Runsten:

- behind a [reverse proxy](./reverse-proxy.md): `https://runsten.example.org/auth/volvo/callback`;
- under a path: `https://example.org/runsten/auth/volvo/callback`;
- without a proxy, on the server itself or through an SSH tunnel: `http://127.0.0.1:8082/auth/volvo/callback`, if Volvo accepts it for a published application (not verified yet).

The application needs the read scopes of the endpoints Runsten polls. `location:read` reportedly requires a review by Volvo; without it, set `RUNSTEN_VOLVO_SCOPES` to the list your application has.

In `.env`:

```sh
RUNSTEN_VOLVO_CLIENT_ID=…
RUNSTEN_VOLVO_CLIENT_SECRET=…
RUNSTEN_VOLVO_API_KEY=…
RUNSTEN_VOLVO_REDIRECT_URI=https://runsten.example.org/auth/volvo/callback
```

The redirect URI in `.env` must be the one registered with the application, on the host your browser uses.

## Connecting your Volvo ID

Open the web interface, sign in, then follow **Connect a Volvo ID** on the home page, and log in with your Volvo ID. The connection and its vehicles are recorded in your account; the tokens are stored encrypted.

The collector then refreshes the tokens before they expire, and at least once a day. A Volvo grant lasts 6 months at most: when it ends, or if Volvo refuses a refresh, the collector stops polling and every page of the vehicle says so, with a **Reconnect the Volvo ID** button.

## The Connection page

The **Connection** page of the web interface tells when the Volvo ID was authorized and renewed, and for each vehicle what the collector did on its latest pass: its polling mode, its latest successful reading and the next one due, an exhausted quota, a pause, its latest failed call.

The pages of a vehicle warn when nothing new is read of it: the grant lost, a pause, a quota, or a collector that has not passed for 15 minutes.
