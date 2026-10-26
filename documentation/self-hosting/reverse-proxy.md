---
description: "Serve Runsten over https behind Caddy or another reverse proxy, on a host of its own, under a path or on the local network only."
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

Then, in `.env`, `RUNSTEN_APP_URL=https://runsten.example.org`, and `docker compose up -d`. The https address makes the session cookie `Secure`; for a Volvo, the application's redirect URI is `https://runsten.example.org/auth/volvo/callback`.

If Caddy runs in a container, attach it to the `runsten_default` network and proxy to `web:8082`.

## Under a path

The proxy strips the prefix, and `RUNSTEN_WEB_BASE_PATH` tells it to the interface; the address includes it:

```text
example.org {
	handle_path /runsten/* {
		reverse_proxy 127.0.0.1:8082
	}
}
```

with `RUNSTEN_WEB_BASE_PATH=/runsten/` and `RUNSTEN_APP_URL=https://example.org/runsten` (for a Volvo, the redirect URI `https://example.org/runsten/auth/volvo/callback`).

## On the local network only

`runsten-api` accepts plain http on a loopback address only: an instance opened from other machines of the local network, by its IP address or a local name, needs https too. No public name is needed: with `tls internal`, Caddy signs the certificate with a certificate authority of its own.

```text
192.168.1.20 {
	tls internal
	reverse_proxy 127.0.0.1:8082
}
```

The site address can be a local name instead, resolved by your router or local DNS (`runsten.home.arpa`). Give the server a fixed address (a static DHCP lease): it goes into `RUNSTEN_APP_URL`, here `https://192.168.1.20`, and into the redirect URI of a Volvo application, which cannot be changed afterwards.

The browsers do not know Caddy's authority, and warn at the first visit. Make each device that opens Runsten trust its root certificate, `root.crt`:

- Caddy on the host installs it in the host's own trust store (`caddy trust`); the file is in its data directory, `/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt` for the Debian package;
- Caddy in a container keeps it in its `/data` volume: `/data/caddy/pki/authorities/local/root.crt`;
- copy it to each device and import it as a trusted authority (the system's certificate settings, or the browser's).

Accepting the warning instead is for a try only: it does not authenticate the server, and whoever stands between the device and the server could present a certificate of their own and read the passwords and the session cookies, should the new warning be accepted too.

For a Volvo, the redirect URI is `https://192.168.1.20/auth/volvo/callback`. Volvo ID only redirects the browser to it and never reaches the server: it works from any device of the network. A self-hoster reports Volvo accepting such a private address; it is not verified otherwise.

Without a proxy, an SSH tunnel brings the server's loopback to your machine: `ssh -L 8082:127.0.0.1:8082 server`, then open `http://127.0.0.1:8082`, with `RUNSTEN_APP_URL=http://127.0.0.1:8082` (and the redirect URI `http://127.0.0.1:8082/auth/volvo/callback`, if Volvo accepts it, not verified yet). Only the machines with an SSH access to the server can open it.

## Good to know

- The proxy must keep the `Host` header (Caddy and most proxies do): `runsten-api` refuses the requests that change state when their `Origin` does not match it.
- Behind the proxy, failed sign-ins are counted per username and for the proxy's address as a whole: someone who fails 10 times delays everyone's next sign-in by up to 15 minutes, but open sessions keep working.
- A `basic_auth` or an address filter at the proxy is not needed; it remains possible as an extra layer, at the price of two logins.
- Never publish the debug page through the proxy: it shows VINs and locations. `runsten-api`'s own port needs no publishing either.
- Without `RUNSTEN_ACCESS_RESTRICTED=true`, `runsten-api` warns when it listens on another address than the loopback. `compose.yaml` sets it, since its ports are published on the host's `127.0.0.1` only.
