<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { mapsShown, showMaps } from '@/shared/lib'

// MapsToggle shows or hides the maps, in this browser; the focus stays on the button
// pressed.
const { t } = useI18n()
const choices = [true, false] as const
</script>

<template>
  <v-btn-toggle
    :model-value="mapsShown"
    mandatory
    density="compact"
    class="segmented maps-toggle"
    role="group"
    :aria-label="t('preferences.maps.title')"
    @update:model-value="showMaps"
  >
    <v-btn v-for="on in choices" :key="String(on)" :value="on" :aria-pressed="on === mapsShown">
      {{ on ? t('preferences.maps.on') : t('preferences.maps.off') }}
    </v-btn>
  </v-btn-toggle>
</template>
