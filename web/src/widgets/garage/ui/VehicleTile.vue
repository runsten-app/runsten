<script setup lang="ts">
import { mdiEye, mdiEyeOff } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { vehicleMode, VehiclePicture, type Vehicle } from '@/entities/vehicle'
import { perHundred, type Amounts, type GarageVehicle } from '@/features/compare-vehicles'
import { useFormat } from '@/shared/lib'
import { LevelBar, TileFigure } from '@/shared/ui'

// VehicleTile is a vehicle as the Garage shows it: its color in the comparison, its name,
// its state of charge and range, what it does now (parked, charging to its target,
// driving), and over the period its distance, its consumption and what 100 km cost it.
// The lowest consumption and cost among the vehicles compared are marked. The tile leads to
// the vehicle's page: its name is the link, stretched over the tile, so that the button
// that hides it from the comparison stays a button of its own.
const props = defineProps<{
  vehicle: Vehicle
  label: string
  color: string
  data: GarageVehicle
  hidden: boolean
  stats: boolean
  bestConsumption: number | null
  bestCost: number | null
}>()
defineEmits<{ toggle: [] }>()
const { t } = useI18n()
const f = useFormat()

// The model under the name the driver gave it (a Tesla's), else nothing: the label is it.
const model = computed(() => {
  const m = props.vehicle.model
  if (!props.vehicle.sheet?.name) return null
  const name = m.variant?.name ?? m.family
  if (!name) return null
  return m.model_year === null ? name : `${name} · ${m.model_year}`
})

const state = computed(() => props.data.state)
const soc = computed(() => state.value?.soc_pct?.value ?? null)
const range = computed(() => state.value?.range_km?.value ?? null)
const level = computed(() =>
  [
    f.quantity(soc.value, 'percent', 0),
    ...(range.value === null ? [] : [f.quantity(range.value, 'km', 0)]),
  ].join(' · '),
)
const doing = computed(() => {
  const s = state.value
  if (!s) return null
  const mode = vehicleMode(s)
  if (mode === 'charging') {
    const target = s.charging.target_soc_pct?.value
    return target === undefined || target === null
      ? t('garage.tile.charging')
      : t('garage.tile.chargingTo', { target: f.quantity(target, 'percent', 0) })
  }
  if (mode === 'driving') return t('garage.tile.driving')
  if (mode === 'parked') return t('garage.tile.parked')
  return null
})

const totals = computed(() => props.data.period?.totals)
const currency = computed(() => props.data.period?.currency ?? null)
const consumption = computed(() => totals.value?.trips.consumption_kwh_per_100km ?? null)
const cost = computed<Amounts | null>(() =>
  totals.value ? perHundred(totals.value.trips, totals.value.charges) : null,
)
// A vehicle that drove nothing over the period is said so, not ranked: no figure of it
// is worse than another's.
const drove = computed(() => (totals.value?.trips.count ?? 0) > 0)
const isBest = (v: number | null, b: number | null) =>
  !props.hidden && v !== null && b !== null && v === b
</script>

<template>
  <v-card flat :class="['vehicle-tile', { hidden }]" :style="{ '--vehicle-color': color }">
    <div class="head">
      <span class="dot" aria-hidden="true" />
      <div class="names">
        <h2 class="name text-title-medium">
          <router-link :to="{ name: 'vehicle', params: { vehicle: vehicle.id } }" class="link">{{
            label
          }}</router-link>
        </h2>
        <p v-if="model" class="model text-body-small text-medium-emphasis mb-0">{{ model }}</p>
      </div>
      <VehiclePicture :vehicle class="picture" />
    </div>
    <div v-if="soc !== null" class="level">
      <LevelBar :value="soc" :label="t('garage.tile.soc')" class="bar" />
      <span class="text-body-small text-medium-emphasis">{{ level }}</span>
    </div>
    <p v-if="doing" class="doing text-body-small mb-0">{{ doing }}</p>
    <template v-if="stats && totals">
      <dl v-if="drove" class="figures">
        <div class="distance">
          <dt class="label">{{ t('garage.tile.distance') }}</dt>
          <dd><TileFigure :text="f.quantity(totals.trips.distance_km, 'km', 0)" size="small" /></dd>
        </div>
        <div class="consumption">
          <dt class="label">{{ t('garage.tile.consumption') }}</dt>
          <dd>
            <TileFigure
              :text="f.quantity(consumption, 'kWhPer100km', 1)"
              :unknown="consumption === null"
              size="small"
            />
            <span v-if="isBest(consumption, bestConsumption)" class="best d-block">
              {{ t('garage.tile.best') }}
            </span>
          </dd>
        </div>
        <div v-if="currency" class="cost">
          <dt class="label">{{ t('garage.tile.cost') }}</dt>
          <dd>
            <TileFigure
              :text="f.amounts(cost, currency).text"
              :spoken="f.amounts(cost, currency).spoken"
              :unknown="cost === null"
              size="small"
            />
            <span v-if="isBest(cost?.max_minor ?? null, bestCost)" class="best d-block">
              {{ t('garage.tile.best') }}
            </span>
          </dd>
        </div>
      </dl>
      <p v-else class="idle text-body-small text-medium-emphasis mb-0">
        {{ t('garage.tile.noTrip') }}
      </p>
    </template>
    <v-btn
      size="small"
      variant="tonal"
      :prepend-icon="hidden ? mdiEye : mdiEyeOff"
      :aria-pressed="hidden"
      class="toggle"
      @click="$emit('toggle')"
    >
      {{ t('garage.tile.hide') }}
    </v-btn>
  </v-card>
</template>

<style scoped>
.vehicle-tile {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 16px;
  border-radius: var(--v-tile-radius);
  min-width: 0;
}
.vehicle-tile.hidden > :not(.toggle) {
  opacity: 0.45;
}
.head {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.dot {
  flex: none;
  inline-size: 10px;
  block-size: 10px;
  margin-block-start: 7px;
  border-radius: 50%;
  background: var(--vehicle-color);
}
.names {
  flex: 1;
  min-width: 0;
}
.name {
  overflow-wrap: anywhere;
}
/* The name leads to the vehicle's page, from anywhere on the tile. */
.link {
  color: inherit;
  text-decoration: none;
}
.link::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: inherit;
}
.link:focus-visible {
  outline: none;
}
.link:focus-visible::after {
  outline: 2px solid rgb(var(--v-theme-primary));
  outline-offset: -2px;
}
.vehicle-tile:hover .link {
  text-decoration: underline;
}
.picture {
  flex: none;
}
.level {
  display: flex;
  align-items: center;
  gap: 8px;
}
.bar {
  flex: 1;
}
.figures {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
  margin: 0;
}
.label {
  font-size: 0.75rem;
  color: rgb(var(--v-theme-text-secondary));
}
dd {
  margin: 0;
}
.best {
  font-size: 0.75rem;
  font-weight: 600;
  color: rgb(var(--v-theme-success));
}
/* Above the stretched link, so that it is pressed on its own. */
.toggle {
  position: relative;
  z-index: 1;
  align-self: flex-start;
}
@media (max-width: 599.98px) {
  .vehicle-tile {
    border-radius: var(--v-tile-radius-phone);
  }
}
</style>
