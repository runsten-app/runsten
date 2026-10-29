<script setup lang="ts">
import { mdiChevronDown, mdiChevronUp, mdiContentSave } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Vehicle } from '@/entities/vehicle'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'
import { ErrorSummary, FactItem } from '@/shared/ui'
import { useSetVehicleModel } from '../composables/useSetVehicleModel'
import { useVariants } from '../composables/useVariants'
import VariantLabel from './VariantLabel.vue'
import {
  automatic,
  byYear,
  chargersOf,
  draftOf,
  effectiveOf,
  field,
  modelOf,
  modelsOf,
  recognizedOf,
  validate,
  withVariant,
  type Issue,
  type ModelDraft,
} from '../model/draft'

// VehicleModelForm is the model of a vehicle, on its settings page: what it reports of itself, and the
// variant and onboard charger its driver chooses, which outrank the recognition. The model
// first, when the variants are of several (a Polestar of the Data Portal is offered its
// brand's), then its variants, those of the vehicle's model year first and the others
// folded. Saved on request; the save keeps the focus where it is and says so in a status
// region.
const props = defineProps<{ vehicle: Vehicle; label: string }>()
const { t } = useI18n()
const f = useFormat()
const { variants, isPending: loading, error: loadError } = useVariants(() => props.vehicle.id)
const { setVehicleModel, isPending, error, reset } = useSetVehicleModel()

const id = computed(() => props.vehicle.id)
const ids = computed(() => ({
  variant: field.variant(id.value),
  variantLegend: `vehicle-${id.value}-variant-legend`,
  models: `vehicle-${id.value}-models`,
  otherYears: `vehicle-${id.value}-other-years`,
  otherYearsToggle: `vehicle-${id.value}-other-years-toggle`,
  charger: field.charger(id.value),
  chargerLegend: `vehicle-${id.value}-charger-legend`,
}))

const draft = ref<ModelDraft>(draftOf(props.vehicle.model))
const options = computed(() => variants.value ?? [])
const recognized = computed(() => recognizedOf(options.value))
const effective = computed(() => effectiveOf(draft.value, options.value))

// The model the variants are shown of: the one pressed, else the draft's or the
// recognition's. Pressing another forgets a variant of the previous one, which would no
// longer show.
const models = computed(() => modelsOf(options.value))
const pressed = ref<string | null>(null)
const model = computed(
  () => pressed.value ?? modelOf(draft.value, options.value, props.vehicle.model.family),
)
const split = computed(() => byYear(options.value, model.value, props.vehicle.model.model_year))
// Mandatory: the toggle never unpresses a model, but its type allows it.
function chooseModel(m: string | null) {
  if (m === null) return
  pressed.value = m
  if (effectiveOf(draft.value, options.value)?.family !== m && draft.value.variant !== automatic)
    chooseVariant(automatic)
}
// The other years open by themselves when they hold the variant chosen.
const othersOpen = ref(false)
watch(
  split,
  (s) => {
    if (s.others.some((o) => o.id === draft.value.variant)) othersOpen.value = true
  },
  { immediate: true },
)
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
function chargerLabel(c: number): string {
  const v = effective.value
  return v && c === v.ac_option_kw
    ? t('vehicleModel.chargerOption', { kw: kw(c) })
    : t('vehicleModel.chargerStandard', { kw: kw(c) })
}
</script>

<template>
  <!-- Under its area's heading, on the page: the vehicle's name is the picker's, in the bar. -->
  <div class="vehicle-model">
    <dl class="facts mb-4">
      <FactItem :label="t('vehicleModel.vin')" :value="vehicle.vin" class="vin" />
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
    <!-- The API's variants decide: a Polestar of the Data Portal reports no family, and
         is offered its brand's. -->
    <p
      v-else-if="options.length === 0 && vehicle.model.family === null"
      class="none text-body-medium"
    >
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
        <div v-if="models.length > 1" class="mb-2">
          <p :id="ids.models" class="text-body-medium mb-1">{{ t('vehicleModel.model') }}</p>
          <v-btn-toggle
            :model-value="model"
            mandatory
            density="compact"
            class="segmented models"
            role="group"
            :aria-labelledby="ids.models"
            @update:model-value="chooseModel"
          >
            <v-btn v-for="m in models" :key="m" :value="m" :aria-pressed="m === model">
              {{ m }}
            </v-btn>
          </v-btn-toggle>
        </div>
        <!-- Two groups of one name: the browser keeps one radio checked across them, and
             the arrows go from one to the other. -->
        <v-radio-group
          :model-value="draft.variant"
          :name="ids.variant"
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
          <v-radio v-for="o in split.fitting" :key="o.id" :value="o.id" class="option">
            <template #label>
              <VariantLabel :option="o" :recognized="recognized?.id === o.id" />
            </template>
          </v-radio>
        </v-radio-group>
        <p v-if="model === null" class="choose-model text-body-medium mb-2">
          {{ t('vehicleModel.chooseModel') }}
        </p>
        <template v-if="split.others.length">
          <v-btn
            :id="ids.otherYearsToggle"
            variant="text"
            class="other-years-toggle mb-2"
            :append-icon="othersOpen ? mdiChevronUp : mdiChevronDown"
            :aria-expanded="othersOpen"
            :aria-controls="ids.otherYears"
            @click="othersOpen = !othersOpen"
          >
            {{ t('vehicleModel.otherYears', { count: split.others.length }) }}
          </v-btn>
          <div :id="ids.otherYears">
            <v-radio-group
              v-if="othersOpen"
              :model-value="draft.variant"
              :name="ids.variant"
              :aria-labelledby="ids.otherYearsToggle"
              class="variants other-years"
              hide-details
              @update:model-value="chooseVariant"
            >
              <v-radio v-for="o in split.others" :key="o.id" :value="o.id" class="option">
                <template #label>
                  <VariantLabel :option="o" :recognized="recognized?.id === o.id" />
                </template>
              </v-radio>
            </v-radio-group>
          </div>
        </template>
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
  </div>
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
/* Seventeen characters at the figures' size are wider than a column: a row of its own. */
.vin {
  grid-column: 1 / -1;
}
/* Four models do not fit a phone on one line: they wrap rather than run off it. */
.models {
  flex-wrap: wrap;
  max-width: 100%;
}
.option {
  align-items: flex-start;
  margin-bottom: 0.5rem;
}
</style>
