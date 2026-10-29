<script setup lang="ts">
import { mdiDelete } from '@mdi/js'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDeleteVehicle } from '../composables/useDeleteVehicle'

// DeleteVehicle deletes a vehicle with its whole history, behind a confirmation that names
// it. Its connection stays, for the account's other vehicles.
const props = defineProps<{ vehicle: string; label: string }>()
const { t } = useI18n()
const { deleteVehicle, isPending, error, reset } = useDeleteVehicle(() => props.vehicle)

const titleId = 'delete-vehicle-title'
const confirming = ref(false)

function ask() {
  reset()
  confirming.value = true
}

async function confirm() {
  try {
    await deleteVehicle()
  } catch {
    // Shown from the error, in the dialog.
  }
}
</script>

<template>
  <!-- Inert behind the dialog, which is teleported out of it. -->
  <section class="delete-vehicle mb-6" :aria-labelledby="titleId" :inert="confirming || undefined">
    <h2 :id="titleId" class="text-title-large mb-2">{{ t('deleteVehicle.title') }}</h2>
    <p class="text-body-medium mb-4">{{ t('deleteVehicle.help') }}</p>
    <v-btn :prepend-icon="mdiDelete" variant="tonal" color="error" class="delete" @click="ask">
      {{ t('deleteVehicle.button', { vehicle: label }) }}
    </v-btn>

    <v-dialog v-model="confirming" max-width="28rem" aria-labelledby="delete-vehicle-dialog-title">
      <v-card>
        <v-card-title id="delete-vehicle-dialog-title" tag="h2" class="text-title-large text-wrap">
          {{ t('deleteVehicle.confirmTitle', { vehicle: label }) }}
        </v-card-title>
        <v-card-text>
          <p class="text-body-medium mb-2">{{ t('deleteVehicle.text') }}</p>
          <p class="text-body-medium">{{ t('deleteVehicle.again') }}</p>
          <v-alert v-if="error" type="error" variant="tonal" class="mt-4">
            {{ t('deleteVehicle.error') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirming = false">{{ t('deleteVehicle.cancel') }}</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :loading="isPending"
            class="confirm-delete"
            @click="confirm"
          >
            {{ t('deleteVehicle.confirm') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </section>
</template>
