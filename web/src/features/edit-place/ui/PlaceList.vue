<script setup lang="ts">
import { mdiMapMarker, mdiPlus } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RouteLocationRaw } from 'vue-router'
import { currentVersion, upcomingVersion, type Place } from '@/entities/place'
import { dayIn, useFormat, type CurrencyUnit } from '@/shared/lib'
import { usePlaces } from '../composables/usePlaces'

// PlaceList is the account's places: each one's name, radius and price of today, a link
// to edit it, and one to add another up to the limit.
const props = defineProps<{
  currency: CurrencyUnit | null
  // The places of an account, at most: the API's, from the settings.
  maxPlaces: number
  // Where a place and a new one are edited.
  to: (place: string) => RouteLocationRaw
}>()
const { t } = useI18n()
const f = useFormat()
const { places, isPending, error } = usePlaces()
const full = computed(() => (places.value?.length ?? 0) >= props.maxPlaces)

// The price holds from midnight in the place's time zone.
function price(p: Place): string {
  const day = dayIn(new Date(), p.time_zone)
  const now = currentVersion(p.tariff, day)
  const v = now ?? upcomingVersion(p.tariff, day)
  if (!v) return ''
  const values = {
    price: f.price(v.price_per_kwh, props.currency),
    date: f.calendarDay(v.valid_from),
  }
  const text = now ? t('settings.places.since', values) : t('settings.places.from', values)
  return v.windows.length ? `${text} · ${t('settings.places.windows', v.windows.length)}` : text
}

function details(p: Place): string {
  return [
    t('settings.places.radius', { radius: f.number(p.radius_m) }),
    price(p),
    p.without_position ? t('settings.places.withoutPosition') : '',
  ]
    .filter(Boolean)
    .join(' · ')
}
</script>

<template>
  <v-alert v-if="error" type="error" variant="tonal">{{ t('settings.places.error') }}</v-alert>
  <v-progress-linear
    v-else-if="isPending"
    indeterminate
    :aria-label="t('settings.places.loading')"
  />
  <template v-else>
    <p v-if="!places?.length" class="empty text-body-large text-medium-emphasis py-4">
      {{ t('settings.places.empty') }}
    </p>
    <!-- A list of links: each one in a listitem, the divider inside it. -->
    <v-list v-else class="tile-list place-list py-0 mb-4">
      <div v-for="(p, i) in places" :key="p.id" role="listitem">
        <v-divider v-if="i > 0" />
        <v-list-item :to="to(p.id)" :prepend-icon="mdiMapMarker" class="place py-3">
          <v-list-item-title class="text-title-medium">{{ p.name }}</v-list-item-title>
          <p class="details text-body-medium text-medium-emphasis">{{ details(p) }}</p>
        </v-list-item>
      </div>
    </v-list>
    <v-btn
      :to="full ? undefined : to('new')"
      :disabled="full"
      :prepend-icon="mdiPlus"
      :aria-describedby="full ? 'places-full' : undefined"
      color="primary"
      variant="flat"
      class="new-place"
    >
      {{ t('settings.places.new') }}
    </v-btn>
    <p v-if="full" id="places-full" class="text-body-medium text-medium-emphasis mt-2">
      {{ t('settings.places.tooMany', { max: maxPlaces }) }}
    </p>
  </template>
</template>
