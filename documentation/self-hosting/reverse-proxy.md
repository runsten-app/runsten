---
description: "Serve Runsten over https behind Caddy or another reverse proxy, on a host of its own or under a path."
---

# Reverse proxy (https)

Runsten authenticates its users, but serves plain http: a reverse proxy brings https, so that passwords and session cookies never travel in clear text. Put it in front of `runsten-web`, which serves the interface and relays `/api/` and `/auth/` to `runsten-api`.

## On a host of its own

An example with [Caddy](https://caddyserver.com/) on the host, which obtains and renews the certificate itself (the name must resolve to the server, ports 80 and 443 open):

```text
runsten.example.org {
	reverse_proxy 127.0.0.1:8082
}
```

Then, in `.env`, `RUNSTEN_VOLVO_REDIRECT_URI=https://runsten.example.org/auth/volvo/callback` (the application registered with the same URI), and `docker compose up -d`. The https redirect URI makes the session cookie `Secure`.

If Caddy runs in a container, attach it to the `runsten_default` network and proxy to `web:8082`.

## Under a path

The proxy strips the prefix, and `RUNSTEN_WEB_BASE_PATH` tells it to the interface; the redirect URI includes it:

```text
example.org {
	handle_path /runsten/* {
		reverse_proxy 127.0.0.1:8082
	}
}
```

with `RUNSTEN_WEB_BASE_PATH=/runsten/` and `RUNSTEN_VOLVO_REDIRECT_URI=https://example.org/runsten/auth/volvo/callback`.

## Good to know

- The proxy must keep the `Host` header (Caddy and most proxies do): `runsten-api` refuses the requests that change state when their `Origin` does not match it.
- Behind the proxy, failed sign-ins are counted per username and for the proxy's address as a whole: someone who fails 10 times delays everyone's next sign-in by up to 15 minutes, but open sessions keep working.
- A `basic_auth` or an address filter at the proxy is not needed; it remains possible as an extra layer, at the price of two logins.
- Never publish the debug page through the proxy: it shows VINs and locations. `runsten-api`'s own port needs no publishing either.
- Without `RUNSTEN_ACCESS_RESTRICTED=true`, `runsten-api` warns when it listens on another address than the loopback. `compose.yaml` sets it, since its ports are published on the host's `127.0.0.1` only.
