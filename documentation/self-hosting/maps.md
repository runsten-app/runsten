---
description: "Show maps in Runsten: choose a tile server in .env, OpenStreetMap's or another provider's, raster tiles or a PMTiles file."
---

# Maps

Runsten shows no map until the instance names a tile server. Without one, a position is its coordinates and a link to OpenStreetMap, opened only on a click. With one, maps show the vehicle's position, a trip's start and end (and its track when imported), a charge's position, and a place, which you can then set by pressing the map.

The map is off by default for your privacy: the browser fetches the tiles from their server itself, so that server sees the reader's IP address and the areas shown.

## Turning them on

In `.env`, set `RUNSTEN_WEB_MAP_TILES` to the tiles' URL, then run `docker compose up -d`:

```sh
RUNSTEN_WEB_MAP_TILES=https://tile.openstreetmap.org/{z}/{x}/{y}.png
```

Reload the page: the maps show. `runsten-web` allows that server in its content security policy, and nothing else needs to change.

- **Raster tiles**: a URL with `{z}`, `{x}` and `{y}`, as above. OpenStreetMap's own tiles suit a personal instance, within their [usage policy](https://operations.osmfoundation.org/policies/tiles/). For more users, take a tile provider, or serve your own.
- **Vector tiles**: a [PMTiles](https://docs.protomaps.com/pmtiles/) file (Protomaps), a URL ending in `.pmtiles`. Its server must answer range requests, with CORS headers that let the instance's address read it.
- **Attribution**: `RUNSTEN_WEB_MAP_ATTRIBUTION` is the text the provider requires, shown in a corner of each map. By default, `© OpenStreetMap contributors`.

Each reader can still hide the maps in this browser: **Preferences**, **Maps**, shown only where the instance has maps.

To turn the maps off, empty `RUNSTEN_WEB_MAP_TILES` and run `docker compose up -d` again.
