<script setup lang="ts">
import {
  mdiArrowRight,
  mdiBatteryCharging,
  mdiCar,
  mdiHelpCircleOutline,
  mdiOpenInNew,
  mdiParking,
} from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  detailsStaleAfter,
  isStale,
  ReadingItem,
  ReadingProvenance,
  vehicleMode,
  type Cable,
  type ChargingStatus,
  type Engine,
  type Mode,
  type Reading,
} from '@/entities/vehicle'
import { useBattery } from '@/features/view-battery'
import { useVehicleState } from '@/features/watch-vehicle-state'
import { isApiError } from '@/shared/api'
import { kilowatts, openStreetMapUrl, useFormat, useMapsOn, useNow, type Unit } from '@/shared/lib'
import { LevelBar, MapView, StatusPill } from '@/shared/ui'

// Verdandi: the current state of a vehicle, each value with how fresh it is. Each tile says
// once when its values were checked and since when they hold, those of its first value;
// a value that differs says its own. The slot adds to the line under the mode (the VIN).
const props = defineProps<{ vehicle: string }>()

const { t } = useI18n()
const f = useFormat()
// With a map, a link to OpenStreetMap would repeat it.
const mapsOn = useMapsOn()
const now = useNow()
const { state, isPending, error } = useVehicleState(() => props.vehicle)

// The battery's estimated capacity, under the readings it rests on: not a reading
// itself, never checked for freshness. Its own failure leaves the link alone: the state
// stays whole.
const { battery, error: batteryError } = useBattery(() => props.vehicle)
const batterySummary = computed(() => {
  const b = battery.value
  if (batteryError.value || !b) return null
  if (!b.current) return t('battery.tile.notEnough')
  return b.deviation_pct === null
    ? f.quantity(b.current.capacity_kwh, 'kWh')
    : t('battery.state.withDeviation', {
        capacity: f.quantity(b.current.capacity_kwh, 'kWh'),
        deviation: f.signedPercent(b.deviation_pct),
      })
})

const connection = computed(() => state.value?.connection ?? { status: 'active' as const })
const mode = computed(() => (state.value ? vehicleMode(state.value) : null))
const checkedStale = computed(() => {
  const c = state.value?.checked_at
  return !!c && isStale(c, now.value, connection.value)
})

const modeIcons = {
  parked: mdiParking,
  driving: mdiCar,
  charging: mdiBatteryCharging,
}

const errorMessage = computed(() =>
  isApiError(error.value, 'not_found') ? t('vehicle.notFound') : t('state.error'),
)

function quantity(r: Reading<number> | null, unit: Unit, convert = (v: number) => v) {
  return r ? f.quantity(convert(r.value), unit) : null
}
// Each label is written out, so that the i18n lint sees every key in use.
const modeLabels = computed<Record<Mode, string>>(() => ({
  parked: t('state.mode.parked'),
  driving: t('state.mode.driving'),
  charging: t('state.mode.charging'),
}))
const statusLabels = computed<Record<ChargingStatus, string>>(() => ({
  idle: t('charging.status.idle'),
  charging: t('charging.status.charging'),
  done: t('charging.status.done'),
  scheduled: t('charging.status.scheduled'),
  discharging: t('charging.status.discharging'),
  error: t('charging.status.error'),
}))
const cableLabels = computed<Record<Cable, string>>(() => ({
  connected: t('charging.cable.connected'),
  disconnected: t('charging.cable.disconnected'),
  fault: t('charging.cable.fault'),
}))
const engineLabels = computed<Record<Engine, string>>(() => ({
  running: t('engine.running'),
  stopped: t('engine.stopped'),
}))

// The charging status is the tile's title pill: done and charging are good news, an
// error is one; the rest is said plainly.
const statusTones: Record<ChargingStatus, 'success' | 'error' | 'neutral'> = {
  idle: 'neutral',
  charging: 'success',
  done: 'success',
  scheduled: 'neutral',
  discharging: 'neutral',
  error: 'error',
}

function label<T extends string>(r: Reading<T> | null, labels: Record<T, string>) {
  return r ? labels[r.value] : null
}

// The reading whose times a tile shows once: its first known value.
const chargingTile = computed(() => {
  const c = state.value?.charging
  return c ? (c.power_w ?? c.type ?? c.cable ?? c.target_soc_pct ?? null) : null
})
const vehicleTile = computed(() => state.value?.engine ?? state.value?.odometer_km ?? null)
</script>

<template>
  <v-alert v-if="error && !state" type="error" variant="tonal">{{ errorMessage }}</v-alert>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('state.loading')" />
  <div v-else-if="state" class="verdandi">
    <v-alert v-if="error" type="warning" variant="tonal" density="compact" class="mb-4">
      {{ t('state.refreshFailed') }}
    </v-alert>

    <div class="meta d-flex flex-wrap align-center">
      <StatusPill :icon="mode ? modeIcons[mode] : mdiHelpCircleOutline" class="mode">
        {{ mode ? modeLabels[mode] : t('state.mode.unknown') }}
      </StatusPill>
      <span>
        <time v-if="state.checked_at" :datetime="state.checked_at">{{
          t('state.lastChecked', { age: f.age(state.checked_at, now) })
        }}</time>
        <template v-else>{{ t('state.neverChecked') }}</template>
      </span>
      <v-chip v-if="checkedStale" size="small" variant="outlined">{{ t('reading.stale') }}</v-chip>
      <slot />
    </div>

    <v-row density="compact" class="tiles">
      <v-col cols="12" md="7">
        <v-card class="tile battery h-100">
          <v-card-title tag="h2">{{ t('state.battery') }}</v-card-title>
          <v-card-text class="tile-body">
            <dl class="hero-row">
              <ReadingItem
                :label="t('state.soc')"
                :reading="state.soc_pct"
                :value="quantity(state.soc_pct, 'percent')"
                :now="now"
                :connection="connection"
                :tile="state.soc_pct"
                size="hero"
              />
              <ReadingItem
                :label="t('state.range')"
                :reading="state.range_km"
                :value="quantity(state.range_km, 'km')"
                :now="now"
                :connection="connection"
                :tile="state.soc_pct"
              />
              <ReadingItem
                :label="t('state.capacity')"
                :reading="state.battery_capacity_kwh"
                :value="quantity(state.battery_capacity_kwh, 'kWh')"
                :now="now"
                :connection="connection"
                :stale-after="detailsStaleAfter"
                :tile="state.soc_pct"
              />
            </dl>
            <div v-if="state.soc_pct">
              <LevelBar
                :value="state.soc_pct.value"
                :target="state.charging.target_soc_pct?.value"
                :label="t('state.soc')"
                class="soc"
              />
              <div class="under-bar d-flex flex-wrap justify-space-between ga-2 mt-2">
                <ReadingProvenance :reading="state.soc_pct" :now="now" :connection="connection" />
                <span v-if="state.charging.target_soc_pct" class="target">
                  {{ t('state.charging.target') }}
                  <strong>{{ quantity(state.charging.target_soc_pct, 'percent') }}</strong>
                </span>
              </div>
            </div>
            <!-- The estimated capacity is not a reading: it is history, derived from the
                 charges. An inset of the battery's tile, not a fifth tile. -->
            <div class="estimated-capacity inset">
              <p v-if="batterySummary" class="mb-0">
                <span class="text-medium-emphasis">{{ t('battery.tile.capacity') }}:</span>
                {{ batterySummary }}
              </p>
              <router-link :to="{ name: 'battery', params: { vehicle } }" class="text-primary">
                {{ t('battery.state.open') }}
                <v-icon :icon="mdiArrowRight" size="16" aria-hidden="true" />
              </router-link>
            </div>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="5">
        <v-card class="tile h-100" data-test="charging">
          <v-card-title tag="h2" class="d-flex align-center justify-space-between ga-2">
            {{ t('state.charging.title') }}
            <StatusPill
              :tone="state.charging.status ? statusTones[state.charging.status.value] : 'neutral'"
              class="charging-status"
            >
              <span class="d-sr-only">{{ t('state.charging.status') }}:</span>
              {{ label(state.charging.status, statusLabels) ?? f.unknown() }}
            </StatusPill>
          </v-card-title>
          <v-card-text class="tile-body">
            <dl class="fields">
              <ReadingItem
                :label="t('state.charging.power')"
                :reading="state.charging.power_w"
                :value="quantity(state.charging.power_w, 'kW', kilowatts)"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
              />
              <ReadingItem
                :label="t('state.charging.type')"
                :reading="state.charging.type"
                :value="state.charging.type?.value ?? null"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
              />
              <ReadingItem
                :label="t('state.charging.cable')"
                :reading="state.charging.cable"
                :value="label(state.charging.cable, cableLabels)"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
              />
              <ReadingItem
                :label="t('state.charging.target')"
                :reading="state.charging.target_soc_pct"
                :value="quantity(state.charging.target_soc_pct, 'percent')"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
              />
            </dl>
            <ReadingProvenance
              v-if="chargingTile"
              :reading="chargingTile"
              :now="now"
              :connection="connection"
              class="footer"
            />
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" sm="6">
        <v-card class="tile h-100">
          <v-card-title tag="h2">{{ t('state.vehicle') }}</v-card-title>
          <v-card-text class="tile-body">
            <dl class="fields">
              <ReadingItem
                :label="t('state.engine')"
                :reading="state.engine"
                :value="label(state.engine, engineLabels)"
                :now="now"
                :connection="connection"
                :tile="vehicleTile"
              />
              <ReadingItem
                :label="t('state.odometer')"
                :reading="state.odometer_km"
                :value="quantity(state.odometer_km, 'km')"
                :now="now"
                :connection="connection"
                :tile="vehicleTile"
              />
            </dl>
            <ReadingProvenance
              v-if="vehicleTile"
              :reading="vehicleTile"
              :now="now"
              :connection="connection"
              class="footer"
            />
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" sm="6">
        <v-card class="tile h-100">
          <v-card-title tag="h2">{{ t('state.position') }}</v-card-title>
          <v-card-text class="tile-body">
            <dl>
              <ReadingItem
                :label="t('state.coordinates')"
                :reading="state.position"
                :value="state.position ? f.coordinates(state.position.value) : null"
                :now="now"
                :connection="connection"
                :tile="state.position"
                size="small"
              />
            </dl>
            <MapView
              v-if="state.position"
              :label="t('state.map')"
              :markers="[{ position: state.position.value, label: t('state.position') }]"
              class="mt-3"
            />
            <div
              v-if="state.position"
              class="footer d-flex flex-wrap align-center justify-space-between ga-3"
            >
              <ReadingProvenance :reading="state.position" :now="now" :connection="connection" />
              <v-btn
                v-if="!mapsOn"
                :href="openStreetMapUrl(state.position.value)"
                target="_blank"
                rel="noreferrer"
                color="surface-variant"
                class="osm"
                :append-icon="mdiOpenInNew"
              >
                {{ t('common.openInOsm') }}
              </v-btn>
            </div>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </div>
</template>

<style scoped>
.meta {
  gap: 8px 14px;
  margin: -4px 0 12px;
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
.tile {
  display: flex;
  flex-direction: column;
}
.tile-body {
  display: flex;
  flex-direction: column;
  gap: 18px;
  flex: 1 1 auto;
}
.hero-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 16px 32px;
}
.fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px 16px;
}
.footer {
  margin-top: auto;
}
.under-bar {
  font-size: 0.75rem;
  color: rgb(var(--v-theme-text-secondary));
}
.target strong {
  font-weight: 500;
  color: rgb(var(--v-theme-on-surface));
}
.inset {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 16px;
  padding: 12px 14px;
  border-radius: var(--v-control-radius);
  background: rgb(var(--v-theme-surface-variant));
  font-size: 0.875rem;
}
.osm {
  color: rgb(var(--v-theme-primary)) !important;
}
</style>
