<script setup lang="ts">
import { mdiClose, mdiContentSave } from '@mdi/js'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { vehicleLabel, type AliasChoice, type Vehicle } from '@/entities/vehicle'
import { isApiError } from '@/shared/api'
import { ErrorSummary } from '@/shared/ui'
import { useSetVehicleAlias } from '../composables/useSetVehicleAlias'
import { aliasField, maxAlias, validate, type AliasIssue } from '../model/alias'

// VehicleAliasForm is the alias the user gives a vehicle, on its settings page: it
// replaces the vehicle's label, here and as its device's name in Home Assistant. An
// empty field, or the button that removes it, names the vehicle after its model again. Saved on request; the
// save keeps the focus where it is and says so in a status region.
const props = defineProps<{ vehicle: Vehicle }>()
const { t } = useI18n()
const { setVehicleAlias, isPending, error, reset } = useSetVehicleAlias()

const id = computed(() => aliasField(props.vehicle.id))
const titleId = computed(() => `vehicle-${props.vehicle.id}-alias-title`)
const hintId = computed(() => `vehicle-${props.vehicle.id}-alias-hint`)
// The label the vehicle takes without an alias.
const derived = computed(() => vehicleLabel({ ...props.vehicle, alias: null }))

const text = ref(props.vehicle.alias ?? '')
const status = ref('')
watch(text, () => {
  status.value = ''
  reset()
})

// Errors show once the form was sent, then follow the changes.
const sent = ref(false)
const checked = computed(() => validate(text.value))
const messages: Record<AliasIssue, () => string> = {
  tooLong: () => t('vehicleAlias.error.tooLong', { max: maxAlias }),
  control: () => t('vehicleAlias.error.control'),
}
const issue = computed(() => (sent.value && checked.value.issue ? checked.value.issue : null))
const fieldError = computed(() => (issue.value ? messages[issue.value]() : undefined))
const serverError = computed(() => {
  const e = error.value
  if (!e) return null
  if (isApiError(e, 'invalid_body'))
    return { text: t('vehicleAlias.error.invalidBody', { message: e.message }) }
  if (isApiError(e, 'not_found')) return { text: t('vehicleAlias.error.notFound') }
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return { text: t('vehicleAlias.error.unavailable') }
  return { text: t('vehicleAlias.error.internal') }
})
const summary = computed(() => [
  ...(fieldError.value ? [{ field: id.value, text: fieldError.value }] : []),
  ...(serverError.value ? [serverError.value] : []),
])
const summaryRef = ref<InstanceType<typeof ErrorSummary> | null>(null)
async function focusSummary() {
  await nextTick()
  summaryRef.value?.focus()
}

async function send(body: AliasChoice) {
  try {
    await setVehicleAlias({ vehicle: props.vehicle.id, alias: body })
    text.value = body.alias ?? ''
    // Said after the field's own watcher has cleared the previous status.
    await nextTick()
    status.value = body.alias ? t('vehicleAlias.saved') : t('vehicleAlias.removed')
    return true
  } catch {
    await focusSummary()
    return false
  }
}

async function submit() {
  sent.value = true
  const body = checked.value.body
  if (!body) return focusSummary()
  await send(body)
}

// Removing the alias takes its button away: the focus goes to the field.
async function remove() {
  sent.value = false
  if (await send({ alias: null })) document.getElementById(id.value)?.focus()
}
</script>

<template>
  <!-- A tile of the vehicle's settings, under its area's heading. -->
  <v-card tag="section" class="vehicle-alias" :aria-labelledby="titleId">
    <v-card-title :id="titleId" tag="h3" class="text-title-large">
      {{ t('vehicleAlias.title') }}
    </v-card-title>
    <v-card-text class="pt-0">
      <v-form novalidate @submit.prevent="submit">
        <ErrorSummary
          v-if="summary.length"
          ref="summaryRef"
          :title="t('vehicleAlias.error.summary')"
          :items="summary"
        />
        <p :id="hintId" class="text-body-medium mb-4">
          {{ t('vehicleAlias.help', { label: derived }) }}
        </p>
        <v-text-field
          :id="id"
          v-model="text"
          :label="t('vehicleAlias.field')"
          :counter="maxAlias"
          :error-messages="fieldError"
          :aria-invalid="fieldError ? 'true' : undefined"
          :aria-describedby="hintId"
          autocomplete="off"
          class="alias mb-2"
        />
        <v-btn
          type="submit"
          color="primary"
          :prepend-icon="mdiContentSave"
          :loading="isPending"
          class="save"
        >
          {{ t('vehicleAlias.save') }}
        </v-btn>
        <v-btn
          v-if="vehicle.alias"
          variant="text"
          :prepend-icon="mdiClose"
          class="remove ms-2"
          @click="remove"
        >
          {{ t('vehicleAlias.remove') }}
        </v-btn>
      </v-form>
      <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.alias {
  max-width: 28rem;
}
</style>
