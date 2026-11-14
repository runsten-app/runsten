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
| `source.polestar-id` | off | The Polestar ID as a source, for a Polestar the Data Portal does not read (outside the EU and the EEA): its connection and its reads, through the Polestar app's unofficial API ([Your Polestar, with the Polestar ID](../guide/polestar-id.md)). Turn it on for your own instance only. |
| `source.bmw` | on | BMW CarData as a source, for a BMW or a Mini: its connection and its reads, through BMW's own API and its stream ([Your BMW or Mini](../guide/bmw.md)). Turn it off should it misbehave on your instance. |
| `source.pleos` | on | The Pleos Vehicle Data API as a source, for a Hyundai, a Kia or a Genesis: its connection and its reads, through the key the owner creates in the Pleos Playground ([Your Hyundai, Kia or Genesis](../guide/hyundai-kia.md)). In beta: turn it off should it misbehave on your instance. |
| `source.skoda` | on | The MyŠkoda Public API as a source, for a Škoda: its connection and its reads, through the key the owner creates in the MyŠkoda app ([Your Škoda](../guide/skoda.md)). Škoda's terms allow a personal use only: an instance read by its owner fits; turn it off on an instance that serves other people. |
| `source.renault` | off | The My Renault app's API as a source, for a Renault, a Dacia or an Alpine: its connection (the email and password of the owner's account, with a one-time code for some) and its reads ([Your Renault, Dacia or Alpine](../guide/renault.md)). Off by default: the API is unofficial and against the app's terms, and many accounts read from one address could get it blocked. Turn it on for your own instance only. Should Renault change the keys of its app, set `RUNSTEN_RENAULT_GIGYA_KEY` and `RUNSTEN_RENAULT_KAMEREON_KEY` (also `RUNSTEN_RENAULT_GIGYA_URL` and `RUNSTEN_RENAULT_KAMEREON_URL`) in `.env`, as renault-api publishes them. |
| `picture.tesla` | on | A Tesla's picture, drawn by Tesla's configurator in its colour and on its wheels ([Vehicle models](../guide/vehicle-models.md)), which `runsten-api` fetches from Tesla's servers. The configurator's address is not documented: turn it off should Tesla change it, or should you rather not call it. |

The [release notes](https://github.com/runsten-app/runsten/releases) tell when a release adds or removes one.

## What a flag is not

- The settings that last, such as the reverse geocoder or the update check, stay variables of their own in `.env.example`: a flag goes away.
- The web interface reads the flags from `runsten-api`, evaluated for your account: it never asks a third party. With `RUNSTEN_FLAGS`, nothing leaves the instance.
