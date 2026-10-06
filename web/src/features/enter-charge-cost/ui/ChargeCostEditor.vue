<script setup lang="ts">
import { mdiCashEdit, mdiContentSave, mdiDelete } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Charge } from '@/entities/charge'
import type { EnteredCostLimits } from '@/entities/settings'
import { isApiError } from '@/shared/api'
import { useFormat, type CurrencyUnit } from '@/shared/lib'
import { ErrorSummary } from '@/shared/ui'
import { useDeleteChargeCost } from '../composables/useDeleteChargeCost'
import { useSetChargeCost } from '../composables/useSetChargeCost'
import { draftOf, field, validate, type CostDraft, type Issue, type Rule } from '../model/validate'

// ChargeCostEditor enters what was paid for a charge, as on a receipt, or deletes it. The
// form opens under its button and checks the API's rules before sending: each error is
// told under its field and, once sent, in a summary that takes the focus. Saved or
// cancelled, the form closes, the focus goes back to its button, and a status region
// says what happened. Deleting an entered cost is confirmed in a dialog, which stays
// mounted when its button goes (the charge, read again, has no entered cost any more).
const props = defineProps<{
  vehicle: string
  charge: Charge
  currency: CurrencyUnit
  // The API's, from the settings.
  limits: EnteredCostLimits
}>()
const { t } = useI18n()
const f = useFormat()
const { setChargeCost, isPending, error, reset } = useSetChargeCost()

const digits = computed(() => props.currency.minor_digits)
const entered = computed(() => props.charge.cost?.source === 'entered')
const open = ref(false)
const draft = ref<CostDraft>(draftOf(null, 0))
const status = ref('')
const sent = ref(false)

watch(
  draft,
  () => {
    status.value = ''
    reset()
  },
  { deep: true },
)

const checked = computed(() => validate(draft.value, digits.value, props.limits))
const issues = computed(() => (sent.value ? checked.value.issues : []))
const messages: Record<Rule, (p: Record<string, number>) => string> = {
  required: () => t('chargeCost.error.required'),
  notNumber: () => t('chargeCost.error.notNumber'),
  range: (p) =>
    t('chargeCost.error.range', {
      min: f.amount(0, props.currency),
      max: f.majorAmount(p.max ?? 0, props.currency),
    }),
  decimals: (p) => t('chargeCost.error.decimals', p.max ?? 0),
  aboveZero: (p) => t('chargeCost.error.aboveZero', { max: f.number(p.max ?? 0) }),
  tooLong: (p) => t('chargeCost.error.tooLong', { max: p.max ?? 0 }),
}
const message = (i: Issue) => messages[i.rule](i.params ?? {})
const errorOf = (id: string) => {
  const i = issues.value.find((i) => i.field === id)
  return i ? message(i) : undefined
}
const labels = computed<Record<string, string>>(() => ({
  [field.amount]: t('chargeCost.amount'),
  [field.energy]: t('chargeCost.energy'),
  [field.note]: t('chargeCost.note'),
}))

const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'invalid_body'))
    return { text: t('chargeCost.error.invalidBody', { message: e.message }) }
  if (isApiError(e, 'not_found')) return { text: t('chargeCost.error.notFound') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('chargeCost.error.unavailable') }
  return { text: t('chargeCost.error.internal') }
})
const summary = computed(() => [
  ...issues.value.map((i) => ({
    field: i.field,
    text: t('chargeCost.error.item', { field: labels.value[i.field] ?? '', message: message(i) }),
  })),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

const toggleId = 'charge-cost-toggle'
async function focusToggle() {
  await nextTick()
  document.getElementById(toggleId)?.focus()
}

// The form starts from the entered cost, if any, and focuses its first field once shown:
// no autofocus (Vuetify's takes the focus back 50 ms later).
async function openForm() {
  draft.value = draftOf(props.charge.cost, digits.value)
  sent.value = false
  open.value = true
  await nextTick()
  reset()
  status.value = ''
  document.getElementById(field.amount)?.focus()
}

async function close(said = '') {
  open.value = false
  await focusToggle()
  status.value = said
}

async function submit() {
  sent.value = true
  const cost = checked.value.cost
  if (!cost) return focusSummary()
  try {
    await setChargeCost({ vehicle: props.vehicle, id: props.charge.id, cost })
    await close(t('chargeCost.saved'))
  } catch {
    await focusSummary()
  }
}

const {
  deleteChargeCost,
  isPending: deletePending,
  error: deleteError,
  reset: resetDelete,
} = useDeleteChargeCost()
const confirming = ref(false)
const deletedNow = ref(false)
const deleteId = 'charge-cost-delete'

function askDelete() {
  status.value = ''
  resetDelete()
  confirming.value = true
}

async function confirmDelete() {
  try {
    await deleteChargeCost({ vehicle: props.vehicle, id: props.charge.id })
    deletedNow.value = true
    confirming.value = false
  } catch {
    // Shown from the error, in the dialog.
  }
}

// Once the dialog closes, the focus goes back to its button; to the form's once the cost
// is deleted (its own button is gone with it). The dialog has no activator to go back to.
watch(confirming, async (v) => {
  if (v) return
  await nextTick()
  if (!deletedNow.value) {
    document.getElementById(deleteId)?.focus()
    return
  }
  deletedNow.value = false
  document.getElementById(toggleId)?.focus()
  status.value = t('chargeCost.deleted')
})

const suffix = computed(() => f.currencySymbol(props.currency.code))
</script>

<template>
  <div class="charge-cost-editor">
    <div class="d-flex flex-wrap ga-2">
      <v-btn
        :id="toggleId"
        :prepend-icon="mdiCashEdit"
        :aria-expanded="open ? 'true' : 'false'"
        :aria-controls="open ? 'charge-cost-form' : undefined"
        variant="tonal"
        color="primary"
        class="toggle"
        @click="open ? close() : openForm()"
      >
        {{ entered ? t('chargeCost.change') : t('chargeCost.enter') }}
      </v-btn>
      <v-btn
        v-if="entered && !open"
        :id="deleteId"
        :prepend-icon="mdiDelete"
        variant="text"
        color="error"
        class="delete-cost"
        @click="askDelete"
      >
        {{ t('chargeCost.delete.button') }}
      </v-btn>
    </div>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-charge-cost-title">
      <v-card>
        <v-card-title id="delete-charge-cost-title" tag="h2" class="text-title-large text-wrap">
          {{ t('chargeCost.delete.title') }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium">{{ t('chargeCost.delete.text') }}</p>
          <v-alert v-if="deleteError" type="error" variant="tonal" class="mt-4">
            {{ t('chargeCost.delete.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">
            {{ t('chargeCost.delete.cancel') }}
          </v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="deletePending"
            class="confirm-delete"
            @click="confirmDelete"
          >
            {{ t('chargeCost.delete.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-form
      v-if="open"
      id="charge-cost-form"
      class="charge-cost-form mt-4"
      novalidate
      @submit.prevent="submit"
    >
      <ErrorSummary
        v-if="summary.length"
        ref="summaryRef"
        :title="t('chargeCost.error.summary')"
        :items="summary"
      />
      <p class="text-body-medium mb-3">{{ t('chargeCost.help') }}</p>
      <v-text-field
        :id="field.amount"
        v-model="draft.amount"
        :label="t('chargeCost.amount')"
        :error-messages="errorOf(field.amount)"
        :aria-invalid="errorOf(field.amount) ? 'true' : undefined"
        :hint="t('chargeCost.amountHelp')"
        persistent-hint
        :suffix
        inputmode="decimal"
        autocomplete="off"
        class="number mb-2"
      />
      <v-text-field
        :id="field.energy"
        v-model="draft.energy"
        :label="t('chargeCost.energy')"
        :error-messages="errorOf(field.energy)"
        :aria-invalid="errorOf(field.energy) ? 'true' : undefined"
        :hint="t('chargeCost.energyHelp')"
        persistent-hint
        suffix="kWh"
        inputmode="decimal"
        autocomplete="off"
        class="number mb-2"
      />
      <v-textarea
        :id="field.note"
        v-model="draft.note"
        :label="t('chargeCost.note')"
        :error-messages="errorOf(field.note)"
        :aria-invalid="errorOf(field.note) ? 'true' : undefined"
        :hint="t('chargeCost.noteHelp')"
        persistent-hint
        :counter="limits.note_chars"
        rows="2"
        auto-grow
        class="note mb-2"
      />
      <div class="d-flex flex-wrap ga-3">
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiContentSave"
          :loading="isPending"
          class="save"
        >
          {{ t('chargeCost.save') }}
        </v-btn>
        <v-btn variant="text" class="cancel" @click="close()">{{ t('chargeCost.cancel') }}</v-btn>
      </div>
    </v-form>
    <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
  </div>
</template>

<style scoped>
.number {
  max-width: 20rem;
}
.note {
  max-width: 32rem;
}
</style>
