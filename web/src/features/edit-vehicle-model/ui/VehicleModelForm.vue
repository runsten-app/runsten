<script setup lang="ts">
import { mdiContentSave } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SourceLevel, VariantOption, Vehicle } from '@/entities/vehicle'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useSetVehicleModel } from '../composables/useSetVehicleModel'
import { useVariants } from '../composables/useVariants'
import {
  automatic,
  chargersOf,
  draftOf,
  effectiveOf,
  field,
  recognizedOf,
  validate,
  withVariant,
  type Issue,
  type ModelDraft,
} from '../model/draft'

// VehicleModelForm is the model of a vehicle, on its settings page: what it reports of itself, and the
// variant and onboard charger its driver chooses, which outrank the recognition. Saved on
// request; the save keeps the focus where it is and says so in a status region.
const props = defineProps<{ vehicle: Vehicle; label: string }>()
const { t } = useI18n()
const f = useFormat()
const { variants, isPending: loading, error: loadError } = useVariants(() => props.vehicle.id)
const { setVehicleModel, isPending, error, reset } = useSetVehicleModel()

const id = computed(() => props.vehicle.id)
const ids = computed(() => ({
  title: `vehicle-${id.value}-title`,
  variant: field.variant(id.value),
  variantLegend: `vehicle-${id.value}-variant-legend`,
  charger: field.charger(id.value),
  chargerLegend: `vehicle-${id.value}-charger-legend`,
}))

const draft = ref<ModelDraft>(draftOf(props.vehicle.model))
const options = computed(() => variants.value ?? [])
const recognized = computed(() => recognizedOf(options.value))
const effective = computed(() => effectiveOf(draft.value, options.value))
// The charger is asked only when the variant in effect has an option.
const chargers = computed(() => {
  const v = effective.value
  return v && v.ac_option_kw !== null ? chargersOf(v) : []
})

const status = ref('')
watch(
  draft,
  () => {
    status.value = ''
    reset()
  },
  { deep: true },
)
function chooseVariant(variant: string | null) {
  draft.value = withVariant(draft.value, variant ?? automatic, options.value)
}

// Errors show once the form was sent, then follow the changes.
const sent = ref(false)
const checked = computed(() => validate(id.value, draft.value, options.value))
const issues = computed(() => (sent.value ? checked.value.issues : []))
const messages: Record<Issue['rule'], () => string> = {
  unknownVariant: () => t('vehicleModel.error.unknownVariant'),
  notACharger: () => t('vehicleModel.error.notACharger'),
}
const errorOf = (field: string) => {
  const i = issues.value.find((i) => i.field === field)
  return i ? messages[i.rule]() : undefined
}
const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'invalid_body'))
    return { text: t('vehicleModel.error.invalidBody', { message: e.message }) }
  if (isApiError(e, 'not_found')) return { text: t('vehicleModel.error.notFound') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('vehicleModel.error.unavailable') }
  return { text: t('vehicleModel.error.internal') }
})
const legends = computed<Record<string, string>>(() => ({
  [ids.value.variant]: t('vehicleModel.variant'),
  [ids.value.charger]: t('vehicleModel.charger'),
}))
const summary = computed(() => [
  ...issues.value.map((i) => ({
    field: i.field,
    text: t('vehicleModel.error.item', {
      field: legends.value[i.field] ?? '',
      message: messages[i.rule](),
    }),
  })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

async function submit() {
  sent.value = true
  const choice = checked.value.choice
  if (!choice) return focusSummary()
  try {
    await setVehicleModel({ vehicle: id.value, choice })
    status.value = t('vehicleModel.saved')
  } catch {
    await focusSummary()
  }
}

const kw = (v: number) => f.quantity(v, 'kW')
const kwh = (v: number | null) => f.quantity(v, 'kWh')
function years(o: VariantOption): string {
  const y = o.model_years
  if (y.to === null) return t('vehicleModel.yearsSince', { from: y.from })
  if (y.to === y.from) return String(y.from)
  return t('vehicleModel.yearsRange', { from: y.from, to: y.to })
}
function figures(o: VariantOption): string {
  const capacity =
    o.net_kwh === null
      ? t('vehicleModel.grossOnly', { gross: kwh(o.gross_kwh) })
      : t('vehicleModel.capacity', { gross: kwh(o.gross_kwh), net: kwh(o.net_kwh) })
  const ac =
    o.ac_option_kw === null
      ? t('vehicleModel.ac', { kw: kw(o.ac_max_kw) })
      : t('vehicleModel.acOption', { kw: kw(o.ac_max_kw), option: kw(o.ac_option_kw) })
  return [capacity, ac, t('vehicleModel.dc', { kw: kw(o.dc_max_kw) })].join(' · ')
}
// sources tells where the figures come from: the manufacturer, public sources, or both,
// naming the figures public sources give.
function sources(o: VariantOption): string {
  const l = o.levels
  const named: [SourceLevel | null, string][] = [
    [l.gross_kwh, t('vehicleModel.figure.gross')],
    [l.net_kwh, t('vehicleModel.figure.net')],
    [l.ac_max_kw, t('vehicleModel.figure.ac')],
    [l.ac_option_kw, t('vehicleModel.figure.acOption')],
    [l.dc_max_kw, t('vehicleModel.figure.dc')],
  ]
  const present = named.filter(([level]) => level !== null)
  const secondary = present.filter(([level]) => level === 'secondary').map(([, name]) => name)
  if (secondary.length === 0) return t('vehicleModel.fromManufacturer')
  if (secondary.length === present.length) return t('vehicleModel.fromPublic')
  return t('vehicleModel.fromBoth', { figures: secondary.join(', ') })
}
function chargerLabel(c: number): string {
  const v = effective.value
  return v && c === v.ac_option_kw
    ? t('vehicleModel.chargerOption', { kw: kw(c) })
    : t('vehicleModel.chargerStandard', { kw: kw(c) })
}
</script>

<template>
  <section class="vehicle-model mb-6" :aria-labelledby="ids.title">
    <h2 :id="ids.title" class="text-title-large mb-2">{{ label }}</h2>
    <dl class="facts mb-4">
      <FactItem :label="t('vehicleModel.vin')" :value="vehicle.vin" />
      <FactItem :label="t('vehicleModel.family')" :value="vehicle.model.family ?? f.unknown()" />
      <FactItem
        :label="t('vehicleModel.modelYear')"
        :value="vehicle.model.model_year === null ? f.unknown() : String(vehicle.model.model_year)"
      />
    </dl>

    <v-alert v-if="loadError" type="error" variant="tonal">
      {{ t('vehicleModel.loadError') }}
    </v-alert>
    <v-progress-linear v-else-if="loading" indeterminate :aria-label="t('vehicleModel.loading')" />
    <p v-else-if="vehicle.model.family === null" class="none text-body-medium">
      {{ t('vehicleModel.notRead') }}
    </p>
    <p v-else-if="options.length === 0" class="none text-body-medium">
      {{ t('vehicleModel.noVariant', { family: vehicle.model.family }) }}
    </p>
    <v-form v-else class="model-form" novalidate @submit.prevent="submit">
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('vehicleModel.error.summary')"
        :items="summary"
      />
      <fieldset class="mb-4">
        <legend :id="ids.variantLegend" class="text-title-small mb-1">
          {{ t('vehicleModel.variant') }}
        </legend>
        <p class="text-body-medium mb-2">{{ t('vehicleModel.variantHelp') }}</p>
        <v-radio-group
          :model-value="draft.variant"
          :aria-labelledby="ids.variantLegend"
          :error-messages="errorOf(ids.variant)"
          class="variants"
          @update:model-value="chooseVariant"
        >
          <v-radio :id="ids.variant" :value="automatic" class="option">
            <template #label>
              <span>
                <span class="d-block text-body-large">{{ t('vehicleModel.automatic') }}</span>
                {{ ' ' }}
                <span class="d-block text-body-medium text-medium-emphasis">
                  {{
                    recognized
                      ? t('vehicleModel.automaticIs', { name: recognized.name })
                      : t('vehicleModel.automaticNone')
                  }}
                </span>
              </span>
            </template>
          </v-radio>
          <v-radio v-for="o in options" :key="o.id" :value="o.id" class="option">
            <template #label>
              <span>
                <span class="d-block text-body-large">
                  {{ t('vehicleModel.option', { name: o.name, years: years(o) }) }}
                  <span v-if="recognized?.id === o.id" class="recognized text-primary">
                    · {{ t('vehicleModel.recognized') }}
                  </span>
                </span>
                {{ ' ' }}
                <span class="d-block text-body-medium">{{ figures(o) }}</span>
                {{ ' ' }}
                <span class="d-block text-body-small text-medium-emphasis">{{ sources(o) }}</span>
              </span>
            </template>
          </v-radio>
        </v-radio-group>
      </fieldset>

      <fieldset v-if="chargers.length" class="mb-4">
        <legend :id="ids.chargerLegend" class="text-title-small mb-1">
          {{ t('vehicleModel.charger') }}
        </legend>
        <p class="text-body-medium mb-2">{{ t('vehicleModel.chargerHelp') }}</p>
        <v-radio-group
          v-model="draft.charger"
          :aria-labelledby="ids.chargerLegend"
          :error-messages="errorOf(ids.charger)"
          class="chargers"
        >
          <v-radio
            :id="ids.charger"
            :value="automatic"
            :label="t('vehicleModel.chargerUnknown', { kw: kw(Math.max(...chargers)) })"
          />
          <v-radio v-for="c in chargers" :key="c" :value="String(c)" :label="chargerLabel(c)" />
        </v-radio-group>
      </fieldset>

      <div class="d-flex flex-wrap align-center ga-3">
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiContentSave"
          :loading="isPending"
          :aria-label="t('vehicleModel.saveNamed', { vehicle: label })"
          class="save"
        >
          {{ t('vehicleModel.save') }}
        </v-btn>
      </div>
    </v-form>
    <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
  </section>
</template>

<style scoped>
fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}
.facts {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr));
  gap: 1rem;
}
.option {
  align-items: flex-start;
  margin-bottom: 0.5rem;
}
</style>
