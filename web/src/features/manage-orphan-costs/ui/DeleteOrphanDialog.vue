<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OrphanCost } from '@/entities/charge'
import { useDeleteOrphanCost } from '../composables/useDeleteOrphanCost'

// DeleteOrphanDialog confirms the deletion of the orphaned cost it is given (open while
// there is one), amount its amount as shown. It is the section's, not the card's: it stays
// mounted when the card goes, and says it deleted the cost.
const props = defineProps<{ orphan: OrphanCost | null; amount: string }>()
const emit = defineEmits<{ cancel: []; deleted: [] }>()
const { t } = useI18n()
const { deleteOrphanCost, isPending, error, reset } = useDeleteOrphanCost()
// What the dialog shows while it fades out, once orphan is null.
const shown = ref(props.amount)
watch(
  () => props.amount,
  (a) => {
    if (props.orphan) shown.value = a
  },
)

async function confirm() {
  if (!props.orphan) return
  try {
    await deleteOrphanCost(props.orphan.id)
    emit('deleted')
  } catch {
    // Shown from error.
  }
}

// Each opening starts without the last one's error.
watch(
  () => props.orphan,
  (o) => {
    if (o) reset()
  },
)
</script>

<template>
  <v-dialog
    :model-value="!!orphan"
    max-width="28rem"
    aria-labelledby="delete-orphan-title"
    @update:model-value="(v: boolean) => !v && emit('cancel')"
  >
    <v-card>
      <v-card-title id="delete-orphan-title" tag="h2" class="text-title-large text-wrap">
        {{ t('orphans.delete.title') }}
      </v-card-title>
      <v-card-text>
        <p class="text-body-medium">{{ t('orphans.delete.text', { cost: shown }) }}</p>
        <v-alert v-if="error" type="error" variant="tonal" class="mt-4">
          {{ t('orphans.delete.error') }}
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="emit('cancel')">{{ t('orphans.delete.cancel') }}</v-btn>
        <v-btn
          color="error"
          variant="flat"
          :loading="isPending"
          class="confirm-delete"
          @click="confirm"
        >
          {{ t('orphans.delete.confirm') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
