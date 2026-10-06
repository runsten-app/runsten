---
description: "When a service does not start or nothing new is read of the car: health, the Connection page and the debug page."
---

# Troubleshooting

## A service does not start

A configuration error stops `api`, `collector` or `web` at startup, with the reason in the logs:

```sh
docker compose ps
docker compose logs api collector web
```

## Health

`docker compose ps` shows each service's health. The images have no shell: the healthcheck runs the binary itself (`/app healthcheck`), which requests `/healthz`. The API and the web interface answer while they serve, the collector while its passes go through. Docker does not restart an unhealthy container; it restarts one that stops.

## Nothing new is read

The **Connection** page of the web interface tells what the collector did on its latest pass over each vehicle ([Your Volvo application](./volvo.md#the-connection-page)):

- **Grant lost**: the Volvo ID must be connected again (a grant lasts 6 months at most).
- **Quota**: the daily call volume of the application is used up; reading resumes when it is renewed. Trips and charges missed meanwhile are found afterwards, as reconstructed.
- **Paused**: the API refused too many calls; the collector waits.
- **Collector stopped**: no pass for 15 minutes. Look at `docker compose logs collector`.

## Debug page

The Connection page is meant for users; the debug page is for developers: every call with its raw response, VINs and locations included. Set `RUNSTEN_DEBUG_ADDR=0.0.0.0:8090` in `.env` (the container side), run `docker compose up -d`, and open `http://127.0.0.1:8090` on the server, or through `ssh -L 8090:127.0.0.1:8090 server`. Never publish it.
