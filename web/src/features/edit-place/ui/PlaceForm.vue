<script setup lang="ts">
import { mdiContentSave, mdiPlus } from '@mdi/js'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave } from 'vue-router'
import type { Place } from '@/entities/place'
import type { DefaultEfficiency, Limits } from '@/entities/settings'
import { isApiError } from '@/shared/api'
import {
  browserTimeZone,
  dayIn,
  formatCoordinate,
  isCalendarDay,
  parseDecimal,
  timeZones,
  useFormat,
  type CurrencyUnit,
  type Position,
} from '@/shared/lib'
import { ErrorSummary, MapView, type MapCircle } from '@/shared/ui'
import { useSavePlace } from '../composables/useSavePlace'
import { useUnpricedCharges } from '../composables/useUnpricedCharges'
import {
  draftOf,
  emptyDraft,
  firstVersionAfter,
  nextVersion,
  type PlaceDraft,
} from '../model/draft'
import { field, validate, type Issue, type Rule } from '../model/validate'
import DeletePlaceButton from './DeletePlaceButton.vue'
import TariffVersionCard from './TariffVersionCard.vue'

// PlaceForm creates or edits a place and its tariff. It checks the API's rules before
// sending: each error is told under its field and, once sent, in a summary at the top
// that takes the focus and leads to the fields. A save keeps the focus where it is and
// says so in a status region.
const props = defineProps<{
  place: Place | null
  currency: CurrencyUnit | null
  // The API's, from the settings.
  limits: Limits
  defaultEfficiency: DefaultEfficiency
  lat?: number
  lon?: number
}>()
const emit = defineEmits<{ saved: [place: Place]; deleted: [] }>()
defineSlots<{
  // Buttons that fill in the position, such as a vehicle's.
  position?: (p: { setPosition: (p: Position, from: string) => void }) => unknown
}>()
const { t } = useI18n()
const f = useFormat()
const { savePlace, isPending, error, reset } = useSavePlace()

const today = dayIn(new Date())
const draft = ref<PlaceDraft>(
  props.place
    ? draftOf(props.place)
    : emptyDraft({
        timeZone: browserTimeZone(),
        today,
        radiusM: props.limits.radius_m.default,
        lat: props.lat,
        lon: props.lon,
      }),
)
// The place's ID once it exists: a new place gets one when first saved.
const id = ref(props.place?.id)
const name = ref(props.place?.name ?? '')

// A change unsaved is asked about before leaving the page.
const saved = ref(JSON.stringify(draft.value))
const dirty = computed(() => JSON.stringify(draft.value) !== saved.value)
onBeforeRouteLeave(() => !dirty.value || window.confirm(t('place.leave')))
const beforeUnload = (e: BeforeUnloadEvent) => {
  if (dirty.value) e.preventDefault()
}
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))

const status = ref('')
watch(
  draft,
  () => {
    status.value = ''
    reset()
  },
  { deep: true },
)

// Errors show once the form was sent, then follow the changes.
const sent = ref(false)
const checked = computed(() => validate(draft.value, props.limits))
const issues = computed(() => (sent.value ? checked.value.issues : []))

const n = (v: number) => f.number(v)
const messages: Record<Rule, (p: Record<string, number>) => string> = {
  required: () => t('place.error.required'),
  notNumber: () => t('place.error.notNumber'),
  range: (p) => t('place.error.range', { min: n(p.min ?? 0), max: n(p.max ?? 0) }),
  aboveZero: (p) => t('place.error.aboveZero', { max: n(p.max ?? 0) }),
  tooLong: (p) => t('place.error.tooLong', { max: p.max ?? 0 }),
  decimals: (p) => t('place.error.decimals', { max: p.max ?? 0 }),
  notDate: () => t('place.error.notDate'),
  notTime: () => t('place.error.notTime'),
  sameDay: () => t('place.error.sameDay'),
  noVersion: () => t('place.error.noVersion'),
  tooMany: (p) => t('place.error.tooMany', { max: p.max ?? 0 }),
  noDay: () => t('place.error.noDay'),
  noMonth: () => t('place.error.noMonth'),
}
const message = (i: Issue) => messages[i.rule](i.params ?? {})
const errorOf = (id: string) => {
  const i = issues.value.find((i) => i.field === id)
  return i ? message(i) : undefined
}

// The summary names each field with its price and window, as their legends do.
const labels = computed<Record<string, string>>(() => ({
  [field.name]: t('place.name'),
  [field.lat]: t('place.lat'),
  [field.lon]: t('place.lon'),
  [field.radius]: t('place.radius'),
  [field.withoutPosition]: t('place.withoutPosition'),
  [field.maxPower]: t('place.maxPower'),
  [field.efficiency]: t('place.efficiency'),
  [field.timeZone]: t('place.timeZone'),
  [field.tariff]: t('place.tariff.title'),
}))
const versionLabels = computed<Record<string, string>>(() => ({
  from: t('place.tariff.validFrom'),
  price: t('place.tariff.price'),
  window: t('place.tariff.windows'),
}))
const windowLabels = computed<Record<string, string>>(() => ({
  days: t('place.window.days'),
  from: t('place.window.from'),
  to: t('place.window.to'),
  months: t('place.window.months'),
  price: t('place.window.price'),
}))
function describe(i: Issue): string {
  const parts: string[] = []
  const kind = i.field.split('-').at(-1) ?? ''
  if (i.where) {
    const from = draft.value.tariff[i.where.version]?.validFrom ?? ''
    parts.push(
      isCalendarDay(from)
        ? t('place.tariff.versionFrom', { date: f.calendarDay(from) })
        : t('place.tariff.versionNew'),
    )
    if (i.where.window === undefined) parts.push(versionLabels.value[kind] ?? '')
    else {
      parts.push(t('place.window.legend', { n: i.where.window + 1 }))
      parts.push(windowLabels.value[kind] ?? '')
    }
  } else parts.push(labels.value[i.field] ?? '')
  return t('place.error.item', { field: parts.join(', '), message: message(i) })
}

const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'without_position_taken'))
    return { field: field.withoutPosition, text: t('place.error.withoutPositionTaken') }
  if (isApiError(e, 'too_many_places'))
    return { text: t('place.error.tooManyPlaces', { max: props.limits.places }) }
  if (isApiError(e, 'invalid_body'))
    return { text: t('place.error.invalidBody', { message: e.message }) }
  if (isApiError(e, 'not_found')) return { text: t('place.error.notFound') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('place.error.unavailable') }
  return { text: t('place.error.internal') }
})
const summary = computed(() => [
  ...issues.value.map((i) => ({ field: i.field, text: describe(i) })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

async function submit() {
  sent.value = true
  const fields = checked.value.fields
  if (!fields) return focusSummary()
  try {
    const place = await savePlace({ id: id.value, fields })
    id.value = place.id
    name.value = place.name
    saved.value = JSON.stringify(draft.value)
    status.value = t('place.saved')
    emit('saved', place)
  } catch {
    await focusSummary()
  }
}

function deleted() {
  saved.value = JSON.stringify(draft.value)
  emit('deleted')
}

function fillPosition(p: Position, said: string) {
  draft.value.lat = formatCoordinate(p.lat)
  draft.value.lon = formatCoordinate(p.lon)
  // After the watcher of the draft, which clears the status.
  void nextTick(() => (status.value = said))
}

function setPosition(p: Position, from: string) {
  fillPosition(p, t('place.positionFilled', { from }))
}

// The circle of the place as entered, once its fields read as a position and a radius.
const circle = computed<MapCircle | null>(() => {
  const lat = parseDecimal(draft.value.lat)
  const lon = parseDecimal(draft.value.lon)
  const radiusM = parseDecimal(draft.value.radius)
  if (lat === null || lon === null || Math.abs(lat) > 90 || Math.abs(lon) > 180) return null
  return { center: { lat, lon }, radiusM: radiusM !== null && radiusM > 0 ? radiusM : 0 }
})

const zones = computed(() => timeZones(draft.value.timeZone))
const priceSuffix = computed(() =>
  props.currency ? `${f.currencySymbol(props.currency.code)}/kWh` : '/kWh',
)
const advanced = ref(draft.value.efficiency !== '')
const versionsFull = computed(() => draft.value.tariff.length >= props.limits.versions_per_place)

async function focusId(id: string) {
  await nextTick()
  document.getElementById(id)?.focus()
}
function addVersion() {
  draft.value.tariff.push(nextVersion(draft.value.tariff, today))
  void focusId(field.validFrom(draft.value.tariff.length - 1))
}
function removeVersion(i: number) {
  draft.value.tariff.splice(i, 1)
  void focusId(field.tariff)
}

// The saved tariff may leave charges of the place without a price: a new place's starts
// today, after the charges that made the user create it. The form offers to start its
// first version on their day, until the draft does.
const { unpriced } = useUnpricedCharges(id)
const startEarlier = computed(() => {
  const u = unpriced.value
  if (!u?.first_day || u.charges === 0) return null
  const index = firstVersionAfter(draft.value.tariff, u.first_day)
  const from = draft.value.tariff[index]?.validFrom
  return from === undefined ? null : { index, day: u.first_day, from, charges: u.charges }
})
function startOn(s: { index: number; day: string }) {
  const v = draft.value.tariff[s.index]
  if (!v) return
  v.validFrom = s.day
  // The button goes with the alert: the focus goes to the day it changed.
  void focusId(field.validFrom(s.index))
  // After the watcher of the draft, which clears the status.
  void nextTick(
    () => (status.value = t('place.tariff.unpriced.moved', { date: f.calendarDay(s.day) })),
  )
}
</script>

<template>
  <v-form class="place-form" novalidate @submit.prevent="submit">
    <ErrorSummary
      v-if="summary.length"
      ref="summaryRef"
      :title="t('place.error.summary')"
      :items="summary"
    />

    <section class="mb-6" aria-labelledby="place-details-title">
      <h2 id="place-details-title" class="text-title-large mb-3">{{ t('place.details') }}</h2>
      <v-text-field
        :id="field.name"
        v-model="draft.name"
        :label="t('place.name')"
        :error-messages="errorOf(field.name)"
        :aria-invalid="errorOf(field.name) ? 'true' : undefined"
        :counter="limits.place_name_chars"
        autocomplete="off"
        class="name mb-2"
      />

      <fieldset class="mb-2">
        <legend class="text-title-small mb-2">{{ t('place.position') }}</legend>
        <div class="d-flex flex-wrap ga-3">
          <v-text-field
            :id="field.lat"
            v-model="draft.lat"
            :label="t('place.lat')"
            :error-messages="errorOf(field.lat)"
            :aria-invalid="errorOf(field.lat) ? 'true' : undefined"
            inputmode="decimal"
            autocomplete="off"
            class="coordinate"
          />
          <v-text-field
            :id="field.lon"
            v-model="draft.lon"
            :label="t('place.lon')"
            :error-messages="errorOf(field.lon)"
            :aria-invalid="errorOf(field.lon) ? 'true' : undefined"
            inputmode="decimal"
            autocomplete="off"
            class="coordinate"
          />
        </div>
        <p class="text-body-small text-medium-emphasis mb-2">{{ t('place.positionHelp') }}</p>
        <MapView
          :label="t('place.map')"
          :circle
          :hint="t('place.mapHelp')"
          pickable
          class="place-map mb-2"
          @pick="fillPosition($event, t('place.mapPicked'))"
        />
        <div class="d-flex flex-wrap ga-2">
          <slot name="position" :set-position />
        </div>
      </fieldset>

      <div class="d-flex flex-wrap ga-3 mt-4">
        <v-text-field
          :id="field.radius"
          v-model="draft.radius"
          :label="t('place.radius')"
          :error-messages="errorOf(field.radius)"
          :aria-invalid="errorOf(field.radius) ? 'true' : undefined"
          :hint="t('place.radiusHelp')"
          persistent-hint
          suffix="m"
          inputmode="decimal"
          autocomplete="off"
          class="number"
        />
        <v-text-field
          :id="field.maxPower"
          v-model="draft.maxPower"
          :label="t('place.maxPower')"
          :error-messages="errorOf(field.maxPower)"
          :aria-invalid="errorOf(field.maxPower) ? 'true' : undefined"
          :hint="t('place.maxPowerHelp')"
          persistent-hint
          suffix="kW"
          inputmode="decimal"
          autocomplete="off"
          class="number"
        />
      </div>

      <v-autocomplete
        :id="field.timeZone"
        :model-value="draft.timeZone"
        :items="zones"
        :label="t('place.timeZone')"
        :error-messages="errorOf(field.timeZone)"
        :aria-invalid="errorOf(field.timeZone) ? 'true' : undefined"
        :hint="t('place.timeZoneHelp')"
        persistent-hint
        class="time-zone mt-4"
        @update:model-value="(z: string | null) => (draft.timeZone = z ?? '')"
      />

      <v-checkbox
        :id="field.withoutPosition"
        v-model="draft.withoutPosition"
        :label="t('place.withoutPosition')"
        :error-messages="errorOf(field.withoutPosition)"
        :aria-invalid="errorOf(field.withoutPosition) ? 'true' : undefined"
        :hint="t('place.withoutPositionHelp')"
        persistent-hint
        class="mt-2"
      />

      <details
        :open="advanced"
        class="advanced mt-4"
        @toggle="advanced = ($event.target as HTMLDetailsElement).open"
      >
        <summary class="text-title-small">{{ t('place.advanced') }}</summary>
        <v-text-field
          :id="field.efficiency"
          v-model="draft.efficiency"
          :label="t('place.efficiency')"
          :error-messages="errorOf(field.efficiency)"
          :aria-invalid="errorOf(field.efficiency) ? 'true' : undefined"
          :hint="
            t('place.efficiencyHelp', {
              ac: f.number(defaultEfficiency.ac),
              dc: f.number(defaultEfficiency.dc),
            })
          "
          persistent-hint
          inputmode="decimal"
          autocomplete="off"
          class="number mt-3"
        />
      </details>
    </section>

    <section class="mb-6" aria-labelledby="place-tariff-title">
      <h2 id="place-tariff-title" class="text-title-large mb-2">{{ t('place.tariff.title') }}</h2>
      <p class="text-body-medium mb-1">{{ t('place.tariff.help') }}</p>
      <p class="text-body-medium mb-4">{{ t('place.tariff.midnight') }}</p>
      <v-alert v-if="startEarlier" type="info" variant="tonal" class="unpriced mb-4">
        <p>
          {{
            t(
              'place.tariff.unpriced.text',
              { n: startEarlier.charges, date: f.calendarDay(startEarlier.from) },
              startEarlier.charges,
            )
          }}
        </p>
        <p class="mt-2">{{ t('place.tariff.unpriced.retroactive') }}</p>
        <v-btn variant="flat" color="primary" class="start-on mt-3" @click="startOn(startEarlier)">
          {{ t('place.tariff.unpriced.startOn', { date: f.calendarDay(startEarlier.day) }) }}
        </v-btn>
      </v-alert>
      <TariffVersionCard
        v-for="(v, i) in draft.tariff"
        :key="v.key"
        :model-value="v"
        :index="i"
        :removable="draft.tariff.length > 1"
        :price-suffix
        :error="errorOf"
        :max-windows="limits.windows_per_version"
        @remove="removeVersion(i)"
      />
      <v-btn
        :id="field.tariff"
        :prepend-icon="mdiPlus"
        :disabled="versionsFull"
        variant="tonal"
        color="primary"
        class="add-version"
        @click="addVersion"
      >
        {{ t('place.tariff.addVersion') }}
      </v-btn>
    </section>

    <div class="d-flex flex-wrap align-center ga-3">
      <v-btn
        type="submit"
        color="primary"
        size="large"
        :prepend-icon="mdiContentSave"
        :loading="isPending"
        class="save"
      >
        {{ t('place.save') }}
      </v-btn>
      <DeletePlaceButton v-if="id" :place="id" :name @deleted="deleted" />
      <p role="status" class="status text-body-medium">{{ status }}</p>
    </div>
  </v-form>
</template>

<style scoped>
fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}
.name,
.time-zone,
.place-map {
  max-width: 32rem;
}
.coordinate {
  flex: 1 1 10rem;
  max-width: 15rem;
}
.number {
  flex: 1 1 12rem;
  max-width: 20rem;
}
.advanced summary {
  cursor: pointer;
  width: fit-content;
}
</style>
