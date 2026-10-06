<script setup lang="ts">
import { mdiVectorPolyline } from '@mdi/js'
import { useI18n } from 'vue-i18n'

// ReconstructedBadge marks an event the collector never saw, and says why to a screen
// reader; a details page shows the explanation in full. Calm, never a warning's color:
// a reconstructed event is as true as a seen one, its times only wider.
const { t } = useI18n()
</script>

<template>
  <v-chip
    size="small"
    variant="flat"
    color="surface-variant"
    :prepend-icon="mdiVectorPolyline"
    class="reconstructed"
    >{{ t('event.reconstructed')
    }}<span class="d-sr-only">: {{ t('event.reconstructedHelp') }}</span></v-chip
  >
</template>
