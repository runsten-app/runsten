---
description: "Turn a function of Runsten on or off on your instance with RUNSTEN_FLAGS, and the flags each release has."
---

# Feature flags

A release may ship a function behind a flag: on or off by default, which you can turn over on your instance without waiting for another release. A flag is made to go: once a function is on everywhere, a later release removes its flag, and the function stays.

Nothing to do by default: each flag has the value its release gives it. To change one, set `RUNSTEN_FLAGS` in `.env`, as `name=true` or `name=false`, separated by commas:

```sh
RUNSTEN_FLAGS=source.tesla=false
```

Then restart the stack (`docker compose up -d`): `runsten-api` and `runsten-collector` read it when they start, and apply it to every account of the instance. A value that is neither `true` nor `false` stops them, with the reason in their logs; a name the release does not have (a flag removed since) is ignored, with a warning.

## The flags

| Flag | Default | What it turns over |
|---|---|---|
| `source.tesla` | on | Tesla as a source, when it ships: its connection, its page and its reads. Until then, it changes nothing. |

The [release notes](https://github.com/runsten-app/runsten/releases) tell when a release adds or removes one.

## What a flag is not

- The settings that last, such as the reverse geocoder or the update check, stay variables of their own in `.env.example`: a flag goes away.
- The web interface reads the flags from `runsten-api`, evaluated for your account: it never asks a third party. With `RUNSTEN_FLAGS`, nothing leaves the instance.
