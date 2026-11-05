<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AAOSLoggingImport from './AAOSLoggingImport.vue'
import AximoteImport from './AximoteImport.vue'
import TeslaMateImport from './TeslaMateImport.vue'
import VolvoDataPortalImport from './VolvoDataPortalImport.vue'

// ImportSources adds to a vehicle's history: from Aximote, for a Volvo from the files of
// the Volvo Data Portal, for a Tesla from a TeslaMate backup, for a car with Google
// built-in from AAOS Logging's export. One tile, each source a part of it with its latest
// report and its form; what was imported is listed apart (ImportList).
const props = withDefaults(
  defineProps<{ vehicle: string; volvo?: boolean; tesla?: boolean; aaos?: boolean }>(),
  // AAOS Logging runs on cars with Google built-in, which no Tesla is.
  { volvo: true, tesla: false, aaos: (p: { tesla?: boolean }) => !p.tesla },
)
const { t } = useI18n()
const titleId = 'import-sources-title'
</script>

<template>
  <v-card tag="section" class="import-sources" :aria-labelledby="titleId">
    <v-card-title :id="titleId" tag="h3" class="text-title-large">
      {{ t('imports.addTitle') }}
    </v-card-title>
    <v-card-text class="pt-0">
      <template v-if="tesla">
        <TeslaMateImport :vehicle="props.vehicle" />
        <v-divider class="my-6" />
      </template>
      <AximoteImport :vehicle="props.vehicle" />
      <template v-if="volvo">
        <v-divider class="my-6" />
        <VolvoDataPortalImport :vehicle="props.vehicle" />
      </template>
      <template v-if="aaos">
        <v-divider class="my-6" />
        <AAOSLoggingImport :vehicle="props.vehicle" />
      </template>
    </v-card-text>
  </v-card>
</template>
