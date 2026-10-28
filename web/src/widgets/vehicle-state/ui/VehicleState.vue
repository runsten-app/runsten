<script setup lang="ts">
import {
  mdiAccessPointNetworkOff,
  mdiBatteryCharging,
  mdiCar,
  mdiHelpCircleOutline,
  mdiOpenInNew,
  mdiParking,
  mdiSleep,
} from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  detailsStaleAfter,
  isStale,
  lastReport,
  noChargeReading,
  ReadingItem,
  ReadingProvenance,
  reportLateAfter,
  vehicleMode,
  type Cable,
  type ChargingStatus,
  type CurrentChargeType,
  type Engine,
  type Mode,
  type Reading,
  type Sleep,
} from '@/entities/vehicle'
import { useVehicle } from '@/features/select-vehicle'
import { useVehicleState } from '@/features/watch-vehicle-state'
import { isApiError } from '@/shared/api'
import { kilowatts, openStreetMapUrl, useFormat, useMapsOn, useNow, type Unit } from '@/shared/lib'
import { LevelBar, MapView, StatusPill, TileFigure } from '@/shared/ui'

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
const { vehicle: current } = useVehicle(() => props.vehicle)
// Where the car reports no capacity (a Polestar of the Data Portal never does), the net
// capacity of its variant, which the energies rest on: Unknown would read as a setup
// gone wrong.
const variantCapacity = computed(() =>
  state.value?.battery_capacity_kwh ? null : (current.value?.model.variant?.net_kwh ?? null),
)

const connection = computed(() => state.value?.connection ?? { status: 'active' as const })
const mode = computed(() => (state.value ? vehicleMode(state.value) : null))
// The car's own latest measure, when it is older than a car that uploads as it goes would
// give: a Polestar parked may send values hours old, however recently they were read.
const reportedLate = computed(() => {
  const s = state.value
  if (!s) return null
  const c = s.charging
  const last = lastReport([
    s.soc_pct,
    s.range_km,
    s.odometer_km,
    s.position,
    s.engine,
    c.status,
    c.cable,
    c.type,
    c.power_w,
    c.target_soc_pct,
  ])
  return last && now.value - Date.parse(last) > reportLateAfter ? last : null
})
const checkedStale = computed(() => {
  const c = state.value?.checked_at
  return !!c && isStale(c, now.value, connection.value)
})

// A car that sleeps (a Tesla) is left alone: its values are those it had when it fell
// asleep, which the line says rather than let them look late. Awake, nothing to say.
const sleep = computed(() => {
  const s = state.value?.sleep
  return s && s.value !== 'awake' ? (s as Reading<Exclude<Sleep, 'awake'>>) : null
})
const sleepIcons = { asleep: mdiSleep, offline: mdiAccessPointNetworkOff }
const sleepLabels = computed(() => ({
  asleep: t('state.sleep.asleep'),
  offline: t('state.sleep.offline'),
}))

const modeIcons = {
  parked: mdiParking,
  driving: mdiCar,
  charging: mdiBatteryCharging,
}

const errorMessage = computed(() =>
  isApiError(error.value, 'not_found') ? t('vehicle.notFound') : t('state.error'),
)

// The power the car measures, else, where the car says no charge runs, none: a power it
// leaves out then is no unknown.
const power = computed(() => {
  const c = state.value?.charging
  if (!c) return null
  if (c.power_w) return { reading: c.power_w, value: quantity(c.power_w, 'kW', kilowatts) }
  const none = noChargeReading(c)
  return none ? { reading: none, value: t('charging.power.none') } : null
})

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
// AC and DC are the same words in every language.
const typeLabels = computed<Record<CurrentChargeType, string>>(() => ({
  AC: 'AC',
  DC: 'DC',
  none: t('charging.type.none'),
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
      <span v-if="sleep" class="sleep d-inline-flex flex-wrap align-center ga-2">
        <StatusPill :icon="sleepIcons[sleep.value]">{{ sleepLabels[sleep.value] }}</StatusPill>
        <time :datetime="sleep.fetched_at">{{
          t('reading.since', { time: f.dateTime(sleep.fetched_at, now) })
        }}</time>
        <span>{{
          sleep.value === 'asleep' ? t('state.sleep.leftAlone') : t('state.sleep.unreachable')
        }}</span>
      </span>
      <time v-if="reportedLate" :datetime="reportedLate" class="reported">{{
        t('state.lastReported', { time: f.dateTime(reportedLate, now) })
      }}</time>
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
              <div v-if="variantCapacity !== null" class="reading variant-capacity">
                <dt class="label">{{ t('state.capacity') }}</dt>
                <dd>
                  <TileFigure
                    class="value"
                    :text="f.quantity(variantCapacity, 'kWh')"
                    size="large"
                  />
                  <span class="details d-block mt-1">{{ t('state.capacityFromVariant') }}</span>
                </dd>
              </div>
              <ReadingItem
                v-else
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
                :reading="power?.reading ?? null"
                :value="power?.value ?? null"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
              />
              <ReadingItem
                :label="t('state.charging.type')"
                :reading="state.charging.type"
                :value="label(state.charging.type, typeLabels)"
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
/* Its note wraps rather than pushing the capacity under the range on a phone. */
.variant-capacity {
  flex: 1 1 0;
  min-width: 5.5rem;
}
.variant-capacity .label {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
  margin-bottom: 4px;
}
.variant-capacity dd {
  margin: 0;
}
.variant-capacity .details {
  font-size: 0.75rem;
  line-height: 1.4;
  color: rgb(var(--v-theme-text-secondary));
}
.osm {
  color: rgb(var(--v-theme-primary)) !important;
}
</style>
