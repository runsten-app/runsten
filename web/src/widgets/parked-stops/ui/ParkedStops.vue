<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ParkedStop } from '@/entities/stats'
import { useParked } from '@/features/view-stats'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'

// ParkedStops are the stops of the period behind the loss while parked (vampire drain),
// newest first: when, how long, the charge lost (and from what to what), the power it
// stands for, the share of it asleep and the outside temperature, with the events around
// it. As TeslaMate's vampire drain, the stops long enough for the rate only, unless all
// is asked (the page's toggle, beside its period): a shorter one counts in the period's total, not in the rate per day, since
// just after a drive a car is still awake and its state of charge settles.
const props = defineProps<{ vehicle: string; from?: string; to?: string; all: boolean }>()
const { t, locale } = useI18n()
const f = useFormat()
const { parked, isPending, error } = useParked(
  () => props.vehicle,
  () => props.from,
  () => props.to,
)
const every = computed(() => parked.value?.items ?? [])
const stops = computed(() => (props.all ? every.value : every.value.filter((s) => s.rated)))
const seconds = (s: number) => f.totalDuration({ min: s, max: s })
// A stop's duration as a vampire drain tells it: hours under a day, days beyond, to a
// tenth ("7.4 hours", "3.7 days"); a total's whole hours would read 88 hours for 3.7 days.
function lasted(s: number): string {
  if (s < 3600) return seconds(s)
  const days = s >= 86400
  return new Intl.NumberFormat(locale.value, {
    style: 'unit',
    unit: days ? 'day' : 'hour',
    unitDisplay: 'long',
    maximumFractionDigits: 1,
  }).format(days ? s / 86400 : s / 3600)
}
const minStop = computed(() => seconds(parked.value?.min_stop_s ?? 0))
const short = computed(() => stops.value.some((s) => !s.rated))
const title = computed(() =>
  props.all
    ? t('parked.stops', stops.value.length)
    : t('parked.rated', { n: stops.value.length, min: minStop.value }, stops.value.length),
)

const loss = (s: ParkedStop) =>
  s.soc_loss_pct === null ? f.unknown() : f.quantity(s.soc_loss_pct, 'percent', 1)
// The events around a stop, as their links read: literal keys, for the linter.
const afterEvent = (kind: string) =>
  kind === 'charge' ? t('parked.after.charge') : t('parked.after.trip')
const beforeEvent = (kind: string) =>
  kind === 'charge' ? t('parked.before.charge') : t('parked.before.trip')
// From what to what, and the energy when the battery's capacity is known.
const lossDetail = (s: ParkedStop) =>
  [
    f.change(s.start_soc_pct, s.end_soc_pct, 'percent'),
    ...(s.soc_loss_kwh === null ? [] : [f.quantity(s.soc_loss_kwh, 'kWh', 2)]),
  ].join(' · ')
const asleep = (s: ParkedStop) =>
  s.asleep_share === null ? f.unknown() : f.quantity(s.asleep_share * 100, 'percent')

const errorMessage = computed(() =>
  isApiError(error.value, 'not_found') ? t('vehicle.notFound') : t('parked.error'),
)
</script>

<template>
  <v-alert v-if="error" type="error" variant="tonal">{{ errorMessage }}</v-alert>
  <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('parked.loading')" />
  <p v-else-if="!every.length" class="none">{{ t('parked.none') }}</p>
  <v-card v-else class="parked-stops">
    <v-card-title tag="h2">{{ title }}</v-card-title>
    <v-card-text>
      <p v-if="!stops.length" class="none">{{ t('parked.noneRated', { min: minStop }) }}</p>
      <!-- Six columns: on a phone the table scrolls within its card, never the page. -->
      <div v-if="stops.length" class="scroll">
        <table class="stops">
          <thead>
            <tr>
              <th scope="col">{{ t('parked.when') }}</th>
              <th scope="col" class="num">{{ t('parked.duration') }}</th>
              <th scope="col" class="num">{{ t('parked.loss') }}</th>
              <th scope="col" class="num">{{ t('parked.power') }}</th>
              <th scope="col" class="num">{{ t('parked.asleep') }}</th>
              <th scope="col" class="num">{{ t('parked.temperature') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="s in stops"
              :key="`${s.before.id}/${s.after.id}`"
              :class="['stop', { short: !s.rated }]"
            >
              <th scope="row">
                <!-- Both ends: a duration in days cannot tell when the car left. The end's
                     date only when it differs. -->
                <span class="d-block"
                  >{{ f.time(s.from) }} → {{ f.time(s.to, f.dayKey(s.from)) }}</span
                >
                <span class="around d-block">
                  <router-link
                    :to="{ name: s.before.kind, params: { vehicle, id: s.before.id } }"
                    class="text-primary"
                    >{{ afterEvent(s.before.kind) }}</router-link
                  >{{ ' · '
                  }}<router-link
                    :to="{ name: s.after.kind, params: { vehicle, id: s.after.id } }"
                    class="text-primary"
                    >{{ beforeEvent(s.after.kind) }}</router-link
                  >
                </span>
              </th>
              <td class="num duration">
                {{ lasted(s.time_s) }}
                <span v-if="!s.rated" class="when d-block">{{ t('parked.notRated') }}</span>
              </td>
              <td class="num loss">
                {{ loss(s) }}
                <span class="when d-block">{{ lossDetail(s) }}</span>
              </td>
              <td class="num power">{{ f.quantity(s.power_w, 'W') }}</td>
              <td class="num asleep">{{ asleep(s) }}</td>
              <td class="num temperature">{{ f.quantity(s.outside_temp_c, 'celsius') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="short" class="note mt-3 mb-0">{{ t('parked.shortNote', { min: minStop }) }}</p>
      <p class="note mt-1 mb-0">{{ t('parked.estimate') }}</p>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.scroll {
  overflow-x: auto;
}
.stops {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  text-align: start;
  vertical-align: top;
  padding: 10px 16px 10px 0;
}
th:last-child,
td:last-child {
  padding-inline-end: 0;
}
thead th {
  font-size: 0.8125rem;
  font-weight: 400;
  color: rgb(var(--v-theme-text-secondary));
  padding-top: 0;
}
tbody th {
  font-weight: 400;
  white-space: nowrap;
}
tbody tr + tr > * {
  border-top: 1px solid rgb(var(--v-theme-line));
}
.num {
  text-align: end;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
/* A stop under the rate's threshold: its figures secondary, as not counted in the rate. */
.short .loss,
.short .power {
  color: rgb(var(--v-theme-text-secondary));
}
.around,
.when,
.note,
.none {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
</style>
