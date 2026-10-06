import { computed, readonly, ref, type ComputedRef } from 'vue'

// MapTiles are the instance's map tiles, which runsten-web writes into the page when it
// has them (RUNSTEN_WEB_MAP_TILES), and the attribution their provider requires, as
// plain text: raster tiles, a URL with {z}, {x} and {y}, or a PMTiles file of vector
// tiles (vector), its path ending in .pmtiles. The CSP allows their origin only then.
export type MapTiles = { url: string; attribution: string; vector: boolean }

const pmtiles = (url: string) => URL.canParse(url) && new URL(url).pathname.endsWith('.pmtiles')

const meta = (name: string) =>
  document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)?.content ?? ''

// mapTiles are null without tiles: no map, nothing fetched from a third party.
export function mapTiles(): MapTiles | null {
  const url = meta('map-tiles')
  return url ? { url, attribution: meta('map-attribution'), vector: pmtiles(url) } : null
}

// The maps can be hidden: the tiles' provider sees the reader's address and the areas
// shown. Shown by default where the instance has tiles; a preference, not a secret:
// localStorage is fine.
const storageKey = 'runsten.maps'
const shown = ref(localStorage.getItem(storageKey) !== 'off')

export const mapsShown = readonly(shown)

export function showMaps(on: boolean) {
  shown.value = on
  localStorage.setItem(storageKey, on ? 'on' : 'off')
}

// useMapsOn tells whether maps are drawn: the instance has tiles and the reader shows
// them. Without, a link to OpenStreetMap stands in for them.
export function useMapsOn(): ComputedRef<boolean> {
  const tiles = mapTiles()
  return computed(() => tiles !== null && mapsShown.value)
}
