<script setup lang="ts">
import { mdiDelete } from '@mdi/js'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDeletePlace } from '../composables/useDeletePlace'

// DeletePlaceButton deletes a place once confirmed in a dialog: Vuetify's is modal, keeps
// the focus inside and gives it back to the button when it closes.
const props = defineProps<{ place: string; name: string }>()
const emit = defineEmits<{ deleted: [] }>()
const { t } = useI18n()
const { deletePlace, isPending, error, reset } = useDeletePlace()
const open = ref(false)

async function confirm() {
  try {
    await deletePlace(props.place)
    open.value = false
    emit('deleted')
  } catch {
    // Shown from error.
  }
}
</script>

<template>
  <v-dialog
    v-model="open"
    max-width="28rem"
    aria-labelledby="delete-place-title"
    @after-leave="reset"
  >
    <template #activator="{ props: activator }">
      <v-btn
        v-bind="activator"
        :prepend-icon="mdiDelete"
        variant="text"
        color="error"
        class="delete-place"
      >
        {{ t('place.delete.button') }}
      </v-btn>
    </template>
    <v-card>
      <v-card-title id="delete-place-title" tag="h2" class="text-title-large text-wrap">
        {{ t('place.delete.title', { name }) }}
      </v-card-title>
      <v-card-text>
        <p class="text-body-medium">{{ t('place.delete.text') }}</p>
        <v-alert v-if="error" type="error" variant="tonal" class="mt-4">
          {{ t('place.delete.error') }}
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="open = false">{{ t('place.delete.cancel') }}</v-btn>
        <v-btn
          color="error"
          variant="flat"
          :loading="isPending"
          class="confirm-delete"
          @click="confirm"
        >
          {{ t('place.delete.confirm') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
