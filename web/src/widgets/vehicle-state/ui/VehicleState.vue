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
import { computed, useId } from 'vue'
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

// Verdandi: the current state of a vehicle, each value with how fresh it is. Each tile says
// once when its values were checked and since when they hold, those of its first value;
// a value that differs says its own. The battery comes first, with the charge as one line
// under its bar; a charge running, discharging or failing takes a tile of its own beside
// it. The aside slot takes the column beside the vehicle's tile (the state of charge over
// the last days).
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

// The power the car measures: its tile shows only while a charge runs, discharges or
// fails, when a power left out is unknown.
const power = computed(() => {
  const p = state.value?.charging.power_w
  return p ? { reading: p, value: quantity(p, 'kW', kilowatts) } : null
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

// The reading whose times a tile shows once: its first known value.
const chargingTile = computed(() => {
  const c = state.value?.charging
  return c ? (c.power_w ?? c.type ?? c.cable ?? c.target_soc_pct ?? null) : null
})
const vehicleTile = computed(() => {
  const s = state.value
  if (!s) return null
  return s.odometer_km ?? s.climate.outside_temp_c ?? s.climate.cabin_temp_c ?? s.software ?? null
})

// The climate, said under the cabin's temperature only while it runs: preconditioning
// first, which runs it too.
const climateRunning = computed(() => {
  const c = state.value?.climate
  if (c?.preconditioning?.value)
    return { reading: c.preconditioning, value: t('state.climate.preconditioning') }
  if (c?.on?.value) return { reading: c.on, value: t('state.climate.climateOn') }
  return null
})
// The battery heated (a Tesla's, in the cold), said only while it is.
const batteryHeating = computed(() => !!state.value?.climate.battery_heater?.value)

// A charge that needs looking at gets its own tile, its figures large; otherwise (idle,
// done, scheduled, unknown) one line under the battery's bar says it. The engine is not
// shown: the mode's pill above says it, read from it.
// Without a status (status and power are read apart), a power above zero says a charge
// runs; a zero one says nothing.
const chargeActive = computed(() => {
  const c = state.value?.charging
  const s = c?.status?.value
  if (s === 'charging' || s === 'discharging' || s === 'error') return true
  return !c?.status && (c?.power_w?.value ?? 0) > 0
})
const chargeTitle = useId()
// The line's facts. The target is the bar's, unless no state of charge draws one.
const chargeFacts = computed(() => {
  type Fact = { key: string; label: string; reading: Reading | null; value: string | null }
  const s = state.value
  if (!s) return []
  const c = s.charging
  const facts: Fact[] = [
    {
      key: 'cable',
      label: t('state.charging.cable'),
      reading: c.cable,
      value: label(c.cable, cableLabels.value),
    },
    {
      key: 'type',
      label: t('state.charging.type'),
      reading: c.type,
      value: label(c.type, typeLabels.value),
    },
  ]
  if (!s.soc_pct)
    facts.push({
      key: 'target',
      label: t('state.charging.target'),
      reading: c.target_soc_pct,
      value: quantity(c.target_soc_pct, 'percent'),
    })
  return facts.map((x) => ({
    ...x,
    stale: !!x.reading && isStale(x.reading.checked_at, now.value, connection.value),
  }))
})
// The reading whose times the line shows: its first value shown, the status first (the
// line shows no power).
const chargeLineTile = computed(() => {
  const s = state.value
  if (!s) return null
  const c = s.charging
  return c.status ?? c.cable ?? c.type ?? (s.soc_pct ? null : c.target_soc_pct)
})
// The line says when its values were checked only where the battery's line does not
// already say it, as a value does in its tile.
const chargeOwn = computed(() => {
  const r = chargeLineTile.value
  const soc = state.value?.soc_pct
  if (!r) return false
  if (!soc) return true
  return (
    f.age(r.checked_at, now.value) !== f.age(soc.checked_at, now.value) ||
    f.dateTime(r.fetched_at, now.value) !== f.dateTime(soc.fetched_at, now.value)
  )
})

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

    <div class="meta d-flex flex-wrap align-center">
      <StatusPill :icon="mode ? modeIcons[mode] : mdiHelpCircleOutline" class="mode">
        {{ mode ? modeLabels[mode] : t('state.mode.unknown') }}
      </StatusPill>
      <span v-if="sleep" class="sleep d-inline-flex flex-wrap align-center ga-2">
        <StatusPill :icon="sleepIcons[sleep.value]">{{ sleepLabels[sleep.value] }}</StatusPill>
        <time :datetime="sleep.fetched_at">{{
          t('reading.since', { time: f.dateTime(sleep.fetched_at, now) })
        }}</time>
        <span>{{
          sleep.value === 'asleep' ? t('state.sleep.leftAlone') : t('state.sleep.unreachable')
        }}</span>
      </span>
      <span>
        <time v-if="state.checked_at" :datetime="state.checked_at">{{
          t('state.lastChecked', { age: f.age(state.checked_at, now) })
        }}</time>
        <template v-else>{{ t('state.neverChecked') }}</template>
      </span>
      <v-chip v-if="checkedStale" size="small" variant="outlined">{{ t('reading.stale') }}</v-chip>
      <time v-if="reportedLate" :datetime="reportedLate" class="reported">{{
        t('state.lastReported', { time: f.dateTime(reportedLate, now) })
      }}</time>
    </div>

    <v-row density="compact" class="tiles">
      <v-col cols="12" :md="chargeActive ? 7 : 12">
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
            <section
              v-if="!chargeActive"
              class="charge-line"
              data-test="charging"
              :aria-labelledby="chargeTitle"
            >
              <div class="d-flex flex-wrap align-center charge-row">
                <h3 :id="chargeTitle" class="charge-title">{{ t('state.charging.title') }}</h3>
                <StatusPill
                  :tone="
                    state.charging.status ? statusTones[state.charging.status.value] : 'neutral'
                  "
                  class="charging-status"
                >
                  <span class="d-sr-only">{{ t('state.charging.status') }}:</span>
                  {{ label(state.charging.status, statusLabels) ?? f.unknown() }}
                </StatusPill>
                <dl class="facts d-flex flex-wrap">
                  <div
                    v-for="fact in chargeFacts"
                    :key="fact.key"
                    class="reading fact"
                    :class="{ stale: fact.stale }"
                  >
                    <dt class="label">{{ fact.label }}</dt>
                    <dd class="value" :class="{ unknown: fact.value === null }">
                      {{ fact.value ?? f.unknown() }}
                    </dd>
                  </div>
                </dl>
              </div>
              <ReadingProvenance
                v-if="chargeOwn && chargeLineTile"
                :reading="chargeLineTile"
                :now="now"
                :connection="connection"
                class="mt-2"
              />
            </section>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col v-if="chargeActive" cols="12" md="5">
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
                v-if="!state.soc_pct"
                :label="t('state.charging.target')"
                :reading="state.charging.target_soc_pct"
                :value="quantity(state.charging.target_soc_pct, 'percent')"
                :now="now"
                :connection="connection"
                :tile="chargingTile"
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

      <v-col cols="12" :md="$slots.aside ? 5 : 12">
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
            <template v-if="state.position && mapsOn">
              <MapView
                :label="t('state.map')"
                :markers="[{ position: state.position.value, label: t('state.position') }]"
              />
              <!-- With a map, the map is the position; its coordinates and times, small. -->
              <dl class="position-line">
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
            </template>
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

      <v-col v-if="$slots.aside" cols="12" md="7">
        <slot name="aside" />
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
.climate-running {
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
.charge-line {
  padding: 12px 16px;
  border-radius: var(--v-control-radius);
  background: rgb(var(--v-theme-surface-variant));
}
.charge-row {
  gap: 8px 16px;
}
.charge-title {
  margin: 0;
  font-size: 0.875rem;
  font-weight: 500;
  line-height: 1.4;
}
.facts {
  margin: 0;
  gap: 4px 20px;
  font-size: 0.875rem;
}
.fact {
  display: flex;
  gap: 6px;
}
.fact .label {
  color: rgb(var(--v-theme-text-secondary));
}
.fact dd {
  margin: 0;
  font-weight: 500;
}
.fact dd.unknown,
.fact.stale dd {
  font-weight: 400;
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
