<script setup lang="ts">
import { mdiDownload } from '@mdi/js'
import { useI18n } from 'vue-i18n'
import { exportPath } from '@/entities/session'

// AccountData downloads all the account's data, whatever its offer, as one JSON file:
// a link, not a fetch, so that the browser saves the file as it comes. The slot adds
// buttons after it.
const { t } = useI18n()
</script>

<template>
  <div class="account-data">
    <p class="text-body-medium mb-4">{{ t('account.export.help') }}</p>
    <div class="d-flex flex-wrap ga-2">
      <v-btn :href="exportPath" download :prepend-icon="mdiDownload" variant="tonal" class="export">
        {{ t('account.export.button') }}
      </v-btn>
      <slot />
    </div>
  </div>
</template>
