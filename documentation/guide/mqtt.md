---
description: "Publish each vehicle's state to your MQTT broker: Home Assistant's entities created by discovery, the topics and their payloads, and how to expose a broker over TLS."
---

# Home Assistant and MQTT

Runsten publishes the state of your vehicles to your MQTT broker: battery, range, charging, plug, engine, odometer and position. Home Assistant creates a device for each vehicle with its entities, without any configuration; Node-RED, openHAB or any MQTT client read the same topics, in plain text.

It only publishes: nothing sent to the broker reaches the car, and Runsten sends it no command.

## Setting the broker

In **Settings → MQTT**:

- **Broker URL**: `mqtt://host:1883`, or `mqtts://host:8883` over TLS. For Home Assistant's Mosquitto add-on, `mqtt://homeassistant.local:1883`; if Runsten runs in Docker and that name does not resolve there, use Home Assistant's IP address.
- **Username** and **Password**, if the broker asks for them. With the Mosquitto add-on, create a Home Assistant user for Runsten (or a login in the add-on's options). The password is stored encrypted, never shown again, and kept when the URL changes to the same host only.
- **Home Assistant discovery**: on by default; off, only the topics below are published.
- **Publish the position**: on by default; off, no position leaves Runsten, and Home Assistant gets no tracker.
- Under **Advanced**: the topic prefix (`runsten`, to change when several instances share a broker), the discovery prefix (`homeassistant`, unless changed in Home Assistant's MQTT integration) and the client ID (empty: Runsten derives one from the account).

Runsten connects within a minute. The section then says since when it is connected, or why it is not:

| Message | What to check |
|---|---|
| The broker could not be reached | the host and port, and that the broker accepts connections from Runsten's address |
| This instance does not publish to that address | the broker must be reachable from the Internet ([below](#a-broker-on-the-internet)) |
| The secure connection failed | the broker's certificate: issued by a public authority, for the name in the URL; or a port that does not speak TLS |
| The broker refused the username or the password | the credentials, and the user's rights on the broker |
| The broker refused the connection | the client ID (another client using it, or a broker limiting its length) |

## When it publishes

After each reading of a vehicle: every 10 minutes while it is parked, every minute while it drives or charges. Only the values that changed are sent; nothing between two readings, since Runsten knows nothing more. At each connection, it publishes everything again.

Every message is retained, with QoS 1: a client that subscribes (Home Assistant after a restart) gets the latest values at once.

## Topics

Under `<prefix>/vehicles/<vehicle id>/`, where the prefix is `runsten` by default and the vehicle ID is Runsten's (a UUID, the one in the web interface's addresses), never the VIN:

| Topic | Value |
|---|---|
| `battery_level` | state of charge, in % |
| `range_km` | range, in km |
| `charging_status` | `idle`, `charging`, `done`, `scheduled`, `discharging` or `error` |
| `plug` | `connected`, `disconnected` or `fault` |
| `charge_type` | `AC` or `DC` |
| `charging_power_kw` | charging power, in kW |
| `target_soc` | charge limit, in % |
| `odometer_km` | odometer, in km |
| `engine` | `running` or `stopped` |
| `location` | `{"latitude": 45.764, "longitude": 4.8357}`, when the position is published |
| `read_at` | the latest reading of the vehicle, in RFC 3339, UTC (`2026-10-04T10:00:00Z`) |

- Numbers are written with a point, as short as they read: `80`, `312.5`, `11.04`.
- **An unknown value is an empty message**, which removes the previous one: unknown is never zero.
- `<prefix>/status` is `online` while Runsten is connected, and `offline` once it stops or loses the connection (its last will).

These topics are a contract, like the API: a change to them is announced in the update notes.

To watch them, with Mosquitto's client:

```sh
mosquitto_sub -h homeassistant.local -u runsten -P '…' -v -t 'runsten/#'
```

## Home Assistant entities

With discovery on, each vehicle is a device named as the web interface names it (its model and model year, never its VIN), with these entities:

| Entity | Type | From |
|---|---|---|
| Battery | sensor, battery, % | `battery_level` |
| Range | sensor, distance, km | `range_km` |
| Charging status | sensor, enum | `charging_status` |
| Charge type | sensor, enum (AC, DC) | `charge_type` |
| Charging power | sensor, power, kW | `charging_power_kw` |
| Charge limit | sensor, % | `target_soc` |
| Odometer | sensor, distance, km, total increasing | `odometer_km` |
| Last read | sensor, timestamp, diagnostic | `read_at` |
| Plug | binary sensor, plug | `plug` |
| Charging | binary sensor, battery charging | `charging_status` |
| Engine | binary sensor, running | `engine` |
| Location | device tracker | `location` |

Their configurations are published under `<discovery prefix>/<component>/runsten_<vehicle id>/<object>/config`. An unknown value shows as *Unknown*; while Runsten is stopped or cut off from the broker, the entities are *Unavailable*, and come back as they were.

Runsten clears what it published, and Home Assistant removes the entities:

- when the broker is removed from the settings, or replaced by another URL: the entities and the values;
- when discovery is turned off, or its prefix changes: the entities (created again under the new prefix);
- when the topic prefix changes: the values under the old one;
- when a vehicle leaves the account: its entities and values;
- when the position is no longer published: the tracker and the position.

This happens as the change is made, while Runsten is connected to the broker: what it could not clear then stays on the broker.

## A broker on the Internet

An instance that serves others from the Internet may publish only to brokers on the Internet, over TLS: the settings then ask for `mqtts://`, and refuse a local address (`192.168.…`, `homeassistant.local`). Your broker, at home behind your router, must then be reached from the Internet. Three ways:

- **Expose your broker over TLS.** Forward a port of your router (8883) to the broker, give the broker a certificate from a public authority for a name that leads to your address (Let's Encrypt, with a dynamic DNS name if your address changes), and keep its password strong. Runsten checks the certificate against the usual authorities: a self-signed one is refused. Mosquitto's add-on takes a certificate in its options (`certfile`, `keyfile`) and listens over TLS on 8883. With Mosquitto of your own, a listener such as:

  ```text
  listener 8883
  certfile /mosquitto/certs/fullchain.pem
  keyfile /mosquitto/certs/privkey.pem
  allow_anonymous false
  password_file /mosquitto/config/passwords
  acl_file /mosquitto/config/acl
  ```

  with an `acl` file that lets Runsten's user write its topics only:

  ```text
  user runsten
  topic write runsten/#
  topic write homeassistant/#
  ```

- **A broker in the cloud** (HiveMQ Cloud, EMQX Cloud…): Runsten publishes to it, and Home Assistant's MQTT integration connects to it rather than to a local broker. Nothing to open at home.
- **A bridge**: your local Mosquitto subscribes to that cloud broker's `runsten/#` and `homeassistant/#` topics (a `connection` with `topic … in`), and Home Assistant stays on the local broker.

A self-hosted instance reaches a broker on your own network directly, in `mqtt://`: none of this is needed.

## Self-hosting

An instance publishes to brokers on its own network by default. `RUNSTEN_MQTT_PRIVATE_BROKERS=false` in `.env` restricts it to brokers on the Internet over TLS, as above: for an instance whose users are not all trusted, so that a broker URL cannot be used to reach the server's network. Runsten checks the addresses again each time it connects: a name may resolve to another one later.

To try the publishing without Home Assistant, `compose.yaml` has an `mqtt` profile that adds a Mosquitto broker, without a password, reachable from the host only (`127.0.0.1:1883`):

```sh
docker compose --profile mqtt up -d
```

Then set `mqtt://mosquitto:1883` in the settings, and watch what is published:

```sh
docker compose exec mosquitto mosquitto_sub -v -t '#'
```

It is meant for trying and testing, not for a real broker: it keeps nothing across restarts and accepts anyone who reaches it.
