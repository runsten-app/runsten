<script setup lang="ts">
import { defineAsyncComponent } from 'vue'
import { mapsShown, mapTiles, type Position } from '@/shared/lib'
import type { MapCircle, MapMarker } from './map/types'

// MapView is a map, where the instance has tiles and the reader has not hidden the maps;
// nothing otherwise. Leaflet is loaded on demand, out of the main bundle.
const MapFrame = defineAsyncComponent(() => import('./map').then((m) => m.MapFrame))
defineProps<{
  label: string
  // hint says, under the map, what pressing it does.
  hint?: string
  markers?: MapMarker[]
  circle?: MapCircle | null
  route?: boolean
  pickable?: boolean
}>()
defineEmits<{ pick: [position: Position] }>()
const tiles = mapTiles()
</script>

<template>
  <div v-if="tiles && mapsShown" class="map-view">
    <MapFrame :tiles :label :markers :circle :route :pickable @pick="$emit('pick', $event)" />
    <p v-if="hint" class="text-body-small text-medium-emphasis mt-1">{{ hint }}</p>
  </div>
</template>
