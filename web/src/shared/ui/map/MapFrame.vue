<script setup lang="ts">
import 'leaflet/dist/leaflet.css'
import L from 'leaflet'
import { leafletLayer } from 'protomaps-leaflet'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import type { MapTiles, Position } from '@/shared/lib'
import type { MapCircle, MapMarker } from './types'

// MapFrame is a Leaflet map of the instance's tiles: markers, a route between them, a
// place's circle, and the position pressed when pickable. Leaflet styles its elements
// through the CSSOM, which the CSP allows; its markers here are SVG, never images.
// Vector tiles (a PMTiles file) are drawn by protomaps-leaflet on canvases, in the
// theme's light or dark flavor and with names in the reader's language.
const props = defineProps<{
  tiles: MapTiles
  label: string
  markers?: MapMarker[]
  circle?: MapCircle | null
  route?: boolean
  pickable?: boolean
}>()
const emit = defineEmits<{ pick: [position: Position] }>()
const theme = useTheme()
const { locale } = useI18n()
const el = ref<HTMLElement>()
let map: L.Map | undefined
let vectorTiles: L.Layer | undefined
let layers: L.LayerGroup | undefined
let viewed = false

const escape = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)
// The theme's colors, which Leaflet takes as values, not as CSS variables.
const color = (host: HTMLElement, name: string) =>
  `rgb(${getComputedStyle(host).getPropertyValue(`--v-theme-${name}`).trim() || '0,0,0'})`
const latLng = (p: Position): L.LatLngTuple => [p.lat, p.lon]
// Five decimals, as the coordinates are shown: about a meter.
const rounded = (n: number) => Math.round(n * 1e5) / 1e5

function draw() {
  if (!map || !layers || !el.value) return
  layers.clearLayers()
  const accent = color(el.value, 'primary')
  const ring = color(el.value, 'surface')
  const bounds = L.latLngBounds([])
  const points = (props.markers ?? []).map((m) => latLng(m.position))
  if (props.circle) {
    const center = L.latLng(latLng(props.circle.center))
    L.circle(center, {
      radius: props.circle.radiusM,
      color: accent,
      weight: 2,
      fillOpacity: 0.15,
      interactive: false,
    }).addTo(layers)
    // Its square, computed without the map's view, which it may not have yet.
    bounds.extend(center.toBounds(props.circle.radiusM * 2))
  }
  if (props.route && points.length > 1)
    L.polyline(points, { color: accent, weight: 3, dashArray: '6 6', interactive: false }).addTo(
      layers,
    )
  for (const m of props.markers ?? []) {
    L.circleMarker(latLng(m.position), {
      radius: 7,
      color: ring,
      weight: 2,
      fillColor: accent,
      fillOpacity: 1,
    })
      .bindTooltip(escape(m.label), { permanent: true, direction: 'top', offset: [0, -8] })
      .addTo(layers)
    bounds.extend(latLng(m.position))
  }
  if (bounds.isValid()) map.fitBounds(bounds, { padding: [32, 32], maxZoom: 16 })
  // Nothing to show yet (a place without a position): the world, to press it.
  else if (!viewed) map.fitWorld()
  viewed = true
}

onMounted(() => {
  if (!el.value) return
  map = L.map(el.value, { attributionControl: false })
  L.control.attribution({ prefix: false }).addTo(map)
  if (!props.tiles.vector)
    L.tileLayer(props.tiles.url, {
      attribution: escape(props.tiles.attribution),
      maxZoom: 19,
      // Providers ask to know which site uses them: the origin, never the page.
      referrerPolicy: 'strict-origin',
    }).addTo(map)
  else drawVector()
  layers = L.layerGroup().addTo(map)
  map.on('click', (e: L.LeafletMouseEvent) => {
    if (props.pickable) emit('pick', { lat: rounded(e.latlng.lat), lon: rounded(e.latlng.lng) })
  })
  draw()
})
// drawVector lays the vector tiles, again when the theme or the language changes: their
// colors and names are drawn into the tiles. They stay under the markers, in Leaflet's
// tile pane.
function drawVector() {
  if (!map) return
  vectorTiles?.remove()
  vectorTiles = leafletLayer({
    url: props.tiles.url,
    flavor: theme.current.value.dark ? 'dark' : 'light',
    lang: locale.value,
    attribution: escape(props.tiles.attribution),
    maxZoom: 19,
    // A GridLayer of Leaflet's, which its types leave out.
  }) as unknown as L.GridLayer
  vectorTiles.addTo(map)
}

watch(() => [props.markers, props.circle, props.route], draw, { deep: true })
watch(
  () => [theme.current.value.dark, locale.value],
  () => props.tiles.vector && drawVector(),
)
onBeforeUnmount(() => map?.remove())
</script>

<template>
  <div
    ref="el"
    role="region"
    :aria-label="label"
    class="map-frame"
    :class="{ dark: theme.current.value.dark && !tiles.vector, pickable }"
  />
</template>

<style scoped>
.map-frame {
  height: 16rem;
  border-radius: var(--v-control-radius);
  overflow: hidden;
  /* Leaflet's panes and controls stack within the map, under the app's bars and dialogs. */
  isolation: isolate;
  background: rgb(var(--v-theme-surface-variant));
}
.pickable {
  cursor: crosshair;
}
/* Raster tiles are drawn light: dark, they are turned, keeping their hues. */
.dark :deep(.leaflet-tile-pane) {
  filter: invert(1) hue-rotate(180deg) brightness(0.9) contrast(0.9);
}
</style>
