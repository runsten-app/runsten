<script setup lang="ts">
import { mdiFan, mdiHeatWave, mdiOpenInNew, mdiSnowflake } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  detailsStaleAfter,
  isStale,
  lastReport,
  ReadingItem,
  ReadingProvenance,
  reportLateAfter,
  vehicleMode,
  type Cable,
  type ChargingStatus,
  type CurrentChargeType,
  type Mode,
  type Reading,
  type Sleep,
  VehiclePicture,
} from '@/entities/vehicle'
import { useChargeInProgress } from '@/features/browse-charges'
import { useVehicle } from '@/features/select-vehicle'
import { useUpdateText, useVehicleState } from '@/features/watch-vehicle-state'
import { isApiError } from '@/shared/api'
import { kilowatts, openStreetMapUrl, useFormat, useMapsOn, useNow, type Unit } from '@/shared/lib'
import { LevelBar, MapView, StatusPill, TileFigure } from '@/shared/ui'
import { extensionSleepActions } from '../extensions'
import OpeningsTile from './OpeningsTile.vue'

// Verdandi: the current state of a vehicle, each value with how fresh it is. Each tile says
// once when its values were checked and since when they hold, those of its first value;
// a value that differs says its own. A vehicle asleep or out of reach says it first, in a
// band of its own above the tiles. The battery comes first, across; then the charge beside
// the doors and windows, the vehicle beside its position on a map; the aside slot ends the
// grid, across (what the vehicle did over the last days).
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
// asleep, which a band above the tiles says rather than let them look late, with what a
// downstream build offers to do about it (wake it up). Awake, nothing to say.
const sleep = computed(() => {
  const s = state.value?.sleep
  return s && s.value !== 'awake' ? (s as Reading<Exclude<Sleep, 'awake'>>) : null
})
const sleepLabels = computed(() => ({
  asleep: t('state.sleep.asleep'),
  offline: t('state.sleep.offline'),
}))

const errorMessage = computed(() =>
  isApiError(error.value, 'not_found') ? t('vehicle.notFound') : t('state.error'),
)

// The power the car measures: its tile shows only while a charge runs, discharges or
// fails, when a power left out is unknown.
const power = computed(() => {
  const p = state.value?.charging.power_w
  return p ? { reading: p, value: quantity(p, 'kW', kilowatts) } : null
})

// What flows, said under the power: current × voltage × phases, as far as the car tells
// them, only while a current flows (a Tesla on DC tells none).
const electricity = computed(() => {
  const c = state.value?.charging
  if (!c?.current_a || c.current_a.value <= 0) return null
  const parts = [f.quantity(c.current_a.value, 'ampere')]
  if (c.voltage_v) parts.push(f.quantity(c.voltage_v.value, 'volt'))
  if (c.phases) parts.push(t('state.charging.phases', { n: c.phases.value }, c.phases.value))
  return parts.join(' × ')
})
// Whether a charge runs: its status says so, or without a status (status and power are
// read apart), a power above zero; a zero one says nothing.
const charging = computed(() => {
  const c = state.value?.charging
  return c?.status ? c.status.value === 'charging' : (c?.power_w?.value ?? 0) > 0
})
// When the car expects the charge to end, from its own time of the estimate: full, or at
// its target, as it says. Only while it charges: a Volvo tells it idle too.
const timeLeft = computed(() => {
  const r = state.value?.charging.time_left
  if (!r || !charging.value) return null
  const end = Date.parse(r.reported_at ?? r.fetched_at) + r.value.duration_s * 1000
  return {
    reading: r,
    label: r.value.to === 'full' ? t('state.charging.fullAt') : t('state.charging.targetAt'),
    value: f.dateTime(new Date(end).toISOString(), now.value),
  }
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

// The charge in progress, followed live on its page: read only while the cable is
// connected or the car charges, when there may be one.
const pluggedIn = computed(() => {
  const c = state.value?.charging
  return c?.cable?.value === 'connected' || c?.status?.value === 'charging'
})
const { charge: open } = useChargeInProgress(() => props.vehicle, pluggedIn)
const openCharge = computed(() =>
  pluggedIn.value && open.value
    ? { name: 'charge', params: { vehicle: props.vehicle, id: open.value.id } }
    : null,
)

// The reading whose times a tile shows once: its first known value shown.
const chargingTile = computed(() => {
  const c = state.value?.charging
  if (!c) return null
  // While nothing charges, its power is not shown: the status dates the tile.
  const first = chargeActive.value ? (c.power_w ?? c.status) : c.status
  return first ?? c.type ?? c.cable ?? c.target_soc_pct ?? null
})
const vehicleTile = computed(() => {
  const s = state.value
  if (!s) return null
  return s.odometer_km ?? s.climate.outside_temp_c ?? s.climate.cabin_temp_c ?? s.software ?? null
})

// The climate, only while it runs: preconditioning first, which runs it too. Its setpoint
// against the cabin's temperature tells which way it goes: half a degree apart or more,
// it heats or cools; closer, it holds. Said in the state's line, for a glance, and under
// the cabin's temperature.
const climateRunning = computed(() => {
  const c = state.value?.climate
  const reading = c?.preconditioning?.value ? c.preconditioning : c?.on?.value ? c.on : null
  if (!c || !reading) return null
  const setpoint = c.setpoint_c?.value ?? null
  const cabin = c.cabin_temp_c?.value ?? null
  const gap = setpoint !== null && cabin !== null ? setpoint - cabin : 0
  const way = gap >= 0.5 ? 'heating' : gap <= -0.5 ? 'cooling' : 'holding'
  const temp = setpoint !== null ? f.quantity(setpoint, 'celsius') : null
  let value: string
  if (reading === c.preconditioning)
    value = temp
      ? t('state.climate.preconditioningTo', { temp })
      : t('state.climate.preconditioning')
  else if (!temp) value = t('state.climate.climateOn')
  else if (way === 'heating') value = t('state.climate.heatingTo', { temp })
  else if (way === 'cooling') value = t('state.climate.coolingTo', { temp })
  else value = t('state.climate.climateAt', { temp })
  return { reading, value, icon: climateIcons[way] }
})
const climateIcons = { heating: mdiHeatWave, cooling: mdiSnowflake, holding: mdiFan }
// The battery heated (a Tesla's, in the cold), said only while it is.
const batteryHeating = computed(() => !!state.value?.climate.battery_heater?.value)

// A charge running, discharging or failing shows its power, large; otherwise (idle, done,
// scheduled, unknown) its tile says its status and the cable, small. The engine is not
// shown: the mode's pill above says it, read from it.
const chargeActive = computed(() => {
  const s = state.value?.charging.status?.value
  return charging.value || s === 'discharging' || s === 'error'
})
const chargeSize = computed(() => (chargeActive.value ? 'large' : 'small'))
const hasOpenings = computed(() => !!(state.value?.doors || state.value?.windows))
// A map of the position, in a tile of its own beside the vehicle's.
const mapShown = computed(() => mapsOn.value && !!state.value?.position)

// The update waiting for the software (a Tesla's): its status, the version it brings, its
// progress while it downloads or installs, and when it is set to install.
const updateText = useUpdateText()
const update = computed(() => {
  const u = state.value?.software_update
  return u ? { reading: u, value: updateText(u.value, now.value) } : null
})
</script>

<template>
  <!-- No state: the aside (the last days) still shows, its data read apart. -->
  <div v-if="error && !state">
    <v-alert type="error" variant="tonal">{{ errorMessage }}</v-alert>
    <div v-if="$slots.aside" class="mt-4"><slot name="aside" /></div>
  </div>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('state.loading')" />
  <div v-else-if="state" class="verdandi">
    <v-alert v-if="error" type="warning" variant="tonal" density="compact" class="mb-4">
      {{ t('state.refreshFailed') }}
    </v-alert>

    <!-- What the vehicle is doing, before any value: its mode, and its sleep when it sleeps,
         large; the climate running and how fresh the values are, small, under it. Tinted
         only while it sleeps or is out of reach, which explains the values below. -->
    <header class="headline" :class="sleep?.value" data-test="headline">
      <p class="headline-title">
        <!-- Asleep, a mode unknown is not worth saying: the sleep says it all. -->
        <span v-if="mode || !sleep" class="mode">{{
          mode ? modeLabels[mode] : t('state.mode.unknown')
        }}</span>
        <span v-if="sleep" class="sleep" role="status">
          <span v-if="mode" aria-hidden="true" class="sep"> · </span>
          <strong>{{ sleepLabels[sleep.value] }}</strong
          >{{ ' '
          }}<time :datetime="sleep.fetched_at">{{
            t('reading.since', { time: f.dateTime(sleep.fetched_at, now) })
          }}</time>
        </span>
      </p>
      <span v-if="climateRunning" class="climate-pill">
        <v-icon :icon="climateRunning.icon" size="16" aria-hidden="true" />{{
          climateRunning.value
        }}</span
      >
      <div v-if="sleep && extensionSleepActions.length" class="sleep-actions">
        <component
          :is="action"
          v-for="(action, i) in extensionSleepActions"
          :key="i"
          :vehicle="vehicle"
          :sleep="sleep.value"
        />
      </div>
      <span v-if="sleep" class="sleep-note">{{
        sleep.value === 'asleep' ? t('state.sleep.leftAlone') : t('state.sleep.unreachable')
      }}</span>
      <span class="freshness d-inline-flex flex-wrap align-center">
        <time v-if="state.checked_at" :datetime="state.checked_at">{{
          t('state.lastChecked', { age: f.age(state.checked_at, now) })
        }}</time>
        <template v-else>{{ t('state.neverChecked') }}</template>
        <v-chip v-if="checkedStale" size="small" variant="outlined">{{
          t('reading.stale')
        }}</v-chip>
        <time v-if="reportedLate" :datetime="reportedLate" class="reported">{{
          t('state.lastReported', { time: f.dateTime(reportedLate, now) })
        }}</time>
      </span>
    </header>

    <v-row density="compact" class="tiles">
      <v-col cols="12">
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
              <!-- Neither reported nor known from a variant: said small, with where to
                   choose the variant, rather than a large Unknown. -->
              <ReadingItem
                v-else-if="!state.battery_capacity_kwh"
                :label="t('state.capacity')"
                :reading="null"
                :value="null"
                :now="now"
                :connection="connection"
                size="small"
                class="no-capacity"
              >
                <router-link
                  :to="{ name: 'vehicleSettings', params: { vehicle } }"
                  class="choose-variant d-block mt-1"
                  >{{ t('state.chooseVariant') }}</router-link
                >
              </ReadingItem>
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
                <span v-if="batteryHeating" class="heater">{{
                  t('state.climate.batteryHeater')
                }}</span>
                <span v-if="state.charging.target_soc_pct" class="target">
                  {{ t('state.charging.target') }}
                  <strong>{{ quantity(state.charging.target_soc_pct, 'percent') }}</strong>
                </span>
              </div>
            </div>
            <!-- Without a state of charge, no line under its bar to say it on. -->
            <div v-if="batteryHeating && !state.soc_pct" class="under-bar">
              <span class="heater">{{ t('state.climate.batteryHeater') }}</span>
            </div>
          </v-card-text>
        </v-card>
      </v-col>

      <!-- The charge and the doors and windows, side by side; the charge across without them. -->
      <v-col cols="12" :md="hasOpenings ? 6 : 12">
        <v-card class="tile charging h-100" data-test="charging">
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
                v-if="chargeActive"
                :label="t('state.charging.power')"
                :reading="power?.reading ?? null"
                :value="power?.value ?? null"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
              >
                <span v-if="electricity" class="electricity d-block mt-1">{{ electricity }}</span>
              </ReadingItem>
              <ReadingItem
                v-if="timeLeft"
                :label="timeLeft.label"
                :reading="timeLeft.reading"
                :value="timeLeft.value"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
                class="time-left"
              />
              <ReadingItem
                :label="t('state.charging.type')"
                :reading="state.charging.type"
                :value="label(state.charging.type, typeLabels)"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
                :size="chargeSize"
              />
              <ReadingItem
                :label="t('state.charging.cable')"
                :reading="state.charging.cable"
                :value="label(state.charging.cable, cableLabels)"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
                :size="chargeSize"
              />
              <ReadingItem
                v-if="!state.soc_pct"
                :label="t('state.charging.target')"
                :reading="state.charging.target_soc_pct"
                :value="quantity(state.charging.target_soc_pct, 'percent')"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
                :size="chargeSize"
              />
              <!-- A setting the owner made (a Tesla's, some Volvos'), said only when told. -->
              <ReadingItem
                v-if="state.charging.current_limit_a"
                :label="t('state.charging.limit')"
                :reading="state.charging.current_limit_a"
                :value="quantity(state.charging.current_limit_a, 'ampere')"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
                size="small"
                class="current-limit"
              />
            </dl>
            <router-link
              v-if="openCharge"
              :to="openCharge"
              class="charge-link d-block text-body-medium text-primary mt-3"
            >
              {{ t('state.charging.follow') }}
            </router-link>
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
      <!-- A source that tells its doors and windows; nothing for the others. -->
      <v-col v-if="hasOpenings" cols="12" md="6">
        <OpeningsTile
          :doors="state.doors"
          :windows="state.windows"
          :now="now"
          :connection="connection"
          class="h-100"
        />
      </v-col>

      <!-- The vehicle, its position on a map beside it where there is one. -->
      <v-col cols="12" :md="mapShown ? 6 : 12">
        <v-card class="tile h-100">
          <v-card-title tag="h2">{{ t('state.vehicle') }}</v-card-title>
          <v-card-text class="tile-body">
            <!-- The vendor's picture, when it gives one (a Volvo's). -->
            <VehiclePicture v-if="current" :vehicle="current" size="large" class="mb-2" />
            <dl class="fields">
              <ReadingItem
                :label="t('state.odometer')"
                :reading="state.odometer_km"
                :value="quantity(state.odometer_km, 'km')"
                :now="now"
                :connection="connection"
                :tile="vehicleTile"
              />
              <!-- A source that tells its temperatures (a Tesla); nothing for the others. -->
              <ReadingItem
                v-if="state.climate.outside_temp_c"
                :label="t('state.climate.outside')"
                :reading="state.climate.outside_temp_c"
                :value="quantity(state.climate.outside_temp_c, 'celsius')"
                :now="now"
                :connection="connection"
                :tile="vehicleTile"
                class="outside"
              />
              <ReadingItem
                v-if="state.climate.cabin_temp_c"
                :label="t('state.climate.cabin')"
                :reading="state.climate.cabin_temp_c"
                :value="quantity(state.climate.cabin_temp_c, 'celsius')"
                :now="now"
                :connection="connection"
                :tile="vehicleTile"
                class="cabin"
              >
                <span v-if="climateRunning" class="climate-running d-block mt-1">{{
                  climateRunning.value
                }}</span>
              </ReadingItem>
              <!-- Without the cabin's temperature, what runs says itself. -->
              <ReadingItem
                v-else-if="climateRunning"
                :label="t('state.climate.title')"
                :reading="climateRunning.reading"
                :value="climateRunning.value"
                :now="now"
                :connection="connection"
                :tile="vehicleTile"
                size="small"
                class="climate"
              />
              <!-- A source that tells its software (a Tesla); nothing for the others. -->
              <ReadingItem
                v-if="state.software"
                :label="t('state.software')"
                :reading="state.software"
                :value="state.software.value"
                :now="now"
                :connection="connection"
                :tile="vehicleTile"
                size="small"
              />
              <ReadingItem
                v-if="update"
                :label="t('state.update.title')"
                :reading="update.reading"
                :value="update.value"
                :now="now"
                :connection="connection"
                :tile="vehicleTile"
                size="small"
                class="update"
              />
              <!-- Without a map, the coordinates are the position: a value of the tile. -->
              <ReadingItem
                v-if="!mapsOn"
                :label="t('state.position')"
                :reading="state.position"
                :value="state.position ? f.coordinates(state.position.value) : null"
                :now="now"
                :connection="connection"
                :tile="vehicleTile"
                size="small"
                class="coordinates"
              />
            </dl>
            <ReadingProvenance
              v-if="vehicleTile"
              :reading="vehicleTile"
              :now="now"
              :connection="connection"
            />
            <div v-if="state.position && !mapsOn" class="footer">
              <v-btn
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

      <v-col v-if="mapShown && state.position" cols="12" md="6">
        <v-card class="tile position h-100" data-test="position">
          <v-card-title tag="h2">{{ t('state.position') }}</v-card-title>
          <v-card-text class="tile-body">
            <MapView
              :label="t('state.map')"
              :markers="[{ position: state.position.value, label: t('state.position') }]"
            />
            <!-- The map is the position; its coordinates and times, small. -->
            <dl class="position-line footer">
              <div class="reading coordinates">
                <dt class="d-sr-only">{{ t('state.coordinates') }}</dt>
                <dd>
                  <span class="value">{{ f.coordinates(state.position.value) }}</span>
                  <ReadingProvenance
                    :reading="state.position"
                    :now="now"
                    :connection="connection"
                  />
                </dd>
              </div>
            </dl>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col v-if="$slots.aside" cols="12">
        <slot name="aside" />
      </v-col>
    </v-row>
  </div>
</template>

<style scoped>
.tile {
  display: flex;
  flex-direction: column;
}
/* The headline: the vehicle's state in one line, its mode, its climate running and how
   fresh the values are, together on the left. Asleep, it takes a second line for why the
   values are old, its actions at the end of the first, and is tinted, so that it reads
   before the values it explains. */
.headline {
  display: flex;
  flex-wrap: wrap;
  /* Its texts of three sizes on one line: on their baseline. */
  align-items: baseline;
  gap: 4px 12px;
  margin: -4px 0 12px;
  padding: 4px 0;
}
.headline > * {
  order: 2;
}
.headline.asleep,
.headline.offline {
  /* The actions' button beside the title: centered on it. */
  align-items: center;
  margin-top: 0;
  padding: 12px 18px;
  border-radius: var(--v-tile-radius);
  background: rgba(var(--v-theme-secondary), 0.12);
  border: 1px solid rgba(var(--v-theme-secondary), 0.28);
}
.headline.offline {
  background: rgb(var(--v-theme-surface-variant));
  border-color: rgb(var(--v-theme-line));
}
.headline-title {
  margin: 0;
  font-size: 1.25rem;
  font-weight: 500;
  line-height: 1.3;
}
.headline.asleep .headline-title,
.headline.offline .headline-title {
  flex: 1 1 14rem;
}
.headline-title .sep {
  white-space: pre;
  color: rgb(var(--v-theme-text-secondary));
}
.headline-title strong {
  font-weight: 600;
}
.headline-title time {
  font-size: 0.875rem;
  font-weight: 400;
  color: rgb(var(--v-theme-text-secondary));
}
.climate-pill {
  font-size: 0.8125rem;
  font-weight: 500;
}
/* Inline, so that its text keeps the line's baseline; the icon sits on it. */
.climate-pill .v-icon {
  vertical-align: -3px;
  margin-right: 4px;
}
.sleep-note,
.freshness {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
.freshness {
  gap: 4px 10px;
}
/* Asleep, the freshness ends its line, under the actions; awake, it follows the mode, so
   that the line reads as one sentence rather than two ends. */
.headline.asleep > .freshness,
.headline.offline > .freshness {
  margin-left: auto;
}
/* Asleep: the actions end the first line; the climate, why the values are old and their
   freshness make the second, under the title. */
.headline.asleep > .sleep-actions,
.headline.offline > .sleep-actions {
  order: 1;
}
.headline.asleep > .headline-title,
.headline.offline > .headline-title {
  order: 0;
}
.headline.asleep > .sleep-note,
.headline.offline > .sleep-note {
  flex: 1 1 auto;
}
.headline.asleep::after,
.headline.offline::after {
  content: '';
  order: 1;
  flex-basis: 100%;
  height: 0;
}
.sleep-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
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
.climate-running,
.electricity {
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
.no-capacity {
  min-width: 7rem;
}
.choose-variant {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-primary));
}
.position-line dd {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 4px 12px;
  font-size: 0.75rem;
  color: rgb(var(--v-theme-text-secondary));
}
.position-line .value {
  font-variant-numeric: tabular-nums;
}
.osm {
  color: rgb(var(--v-theme-primary)) !important;
}
</style>
