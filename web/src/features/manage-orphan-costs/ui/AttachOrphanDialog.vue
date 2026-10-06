<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Charge, OrphanCost } from '@/entities/charge'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'
import { useAttachOrphanCost } from '../composables/useAttachOrphanCost'
import { useCandidateCharges } from '../composables/useCandidateCharges'

// AttachOrphanDialog gives the orphaned cost it is given (open while there is one) to a
// charge the user picks among those of its vehicle around its window (candidatePeriod),
// newest first. Nothing is picked for them: several charges may fit, which is why the
// cost was left without one. It is the section's, not the card's: it stays mounted when
// the card goes, and says it attached the cost.
const props = defineProps<{ orphan: OrphanCost | null }>()
const emit = defineEmits<{ cancel: []; attached: [] }>()
const { t } = useI18n()
const f = useFormat()

// What the dialog shows while it fades out, once orphan is null.
const current = ref<OrphanCost | null>(props.orphan)
// Each opening starts afresh: nothing chosen, no error.
watch(
  () => props.orphan,
  (o) => {
    if (!o) return
    current.value = o
    chosen.value = null
    reset()
  },
)
const chosen = ref<string | null>(null)
const {
  charges,
  isPending,
  error: loadError,
} = useCandidateCharges(
  () => current.value,
  () => !!props.orphan,
)
const { attachOrphanCost, isPending: attaching, error, reset } = useAttachOrphanCost()

// option tells a charge apart from its neighbours: when, type, energy, and its cost.
function option(c: Charge): string {
  const parts = [
    f.bounds({ after: c.start.after, before: c.end.before }),
    c.type ?? t('orphans.attach.typeUnknown'),
    f.quantity(c.energy_soc_kwh, 'kWh'),
  ]
  if (c.cost?.source === 'entered')
    parts.push(t('orphans.attach.entered', { cost: f.chargeCost(c.cost).text }))
  else if (c.cost) parts.push(f.chargeCost(c.cost).text)
  return parts.join(' · ')
}

const message = computed(() => {
  const e = error.value
  if (!e) return ''
  if (isApiError(e, 'charge_has_cost')) return t('orphans.attach.hasCost')
  if (isApiError(e, 'not_found')) return t('orphans.attach.notFound')
  return t('orphans.attach.error')
})

async function attach() {
  const o = current.value
  if (!o || !chosen.value) return
  try {
    await attachOrphanCost({ orphan: o.id, vehicle: o.vehicle_id, charge: chosen.value })
    emit('attached')
  } catch {
    // Shown from error.
  }
}
</script>

<template>
  <v-dialog
    :model-value="!!orphan"
    max-width="36rem"
    aria-labelledby="attach-orphan-title"
    @update:model-value="(v: boolean) => !v && emit('cancel')"
  >
    <v-card>
      <v-card-title id="attach-orphan-title" tag="h2" class="text-title-large text-wrap">
        {{ t('orphans.attach.title') }}
      </v-card-title>
      <v-card-text v-if="current">
        <p class="text-body-medium mb-4">
          {{ t('orphans.attach.help', { window: f.bounds(current.window) }) }}
        </p>
        <v-alert v-if="loadError" type="error" variant="tonal">
          {{ t('orphans.attach.loadError') }}
        </v-alert>
        <v-progress-linear
          v-else-if="isPending"
          indeterminate
          :aria-label="t('orphans.attach.loading')"
        />
        <p v-else-if="!charges?.length" class="none text-body-medium">
          {{ t('orphans.attach.none') }}
        </p>
        <v-radio-group
          v-else
          v-model="chosen"
          :label="t('orphans.attach.choose')"
          hide-details
          class="candidates"
          @update:model-value="reset"
        >
          <v-radio v-for="c in charges" :key="c.id" :value="c.id" :label="option(c)" />
        </v-radio-group>
        <v-alert v-if="message" type="error" variant="tonal" class="mt-4">{{ message }}</v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="emit('cancel')">{{ t('orphans.attach.cancel') }}</v-btn>
        <v-btn
          color="primary"
          variant="flat"
          :disabled="!chosen"
          :loading="attaching"
          class="confirm-attach"
          @click="attach"
        >
          {{ t('orphans.attach.confirm') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
