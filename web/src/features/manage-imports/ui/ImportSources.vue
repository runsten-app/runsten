<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AximoteImport from './AximoteImport.vue'
import VolvoDataPortalImport from './VolvoDataPortalImport.vue'

// ImportSources adds to a vehicle's history: from Aximote or, for a Volvo, from the files
// of the Volvo Data Portal. One tile, each source a part of it with its latest report and
// its form; what was imported is listed apart (ImportList).
const props = withDefaults(defineProps<{ vehicle: string; volvo?: boolean }>(), { volvo: true })
const { t } = useI18n()
const titleId = 'import-sources-title'
</script>

<template>
  <v-card tag="section" class="import-sources" :aria-labelledby="titleId">
    <v-card-title :id="titleId" tag="h3" class="text-title-large">
      {{ t('imports.addTitle') }}
    </v-card-title>
    <v-card-text class="pt-0">
      <AximoteImport :vehicle="props.vehicle" />
      <template v-if="volvo">
        <v-divider class="my-6" />
        <VolvoDataPortalImport :vehicle="props.vehicle" />
      </template>
    </v-card-text>
  </v-card>
</template>
