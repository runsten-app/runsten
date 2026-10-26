<script setup lang="ts">
import { mdiOpenInNew } from '@mdi/js'
import { useI18n } from 'vue-i18n'
import { useInstance } from '../composables/useInstance'

// ReleaseInfo tells the release the instance runs and, when the instance checks for
// them, a newer one with its notes and how to update. Without the check, the releases'
// page: nothing is asked of GitHub but by whoever opens it.
const { t } = useI18n()
const { instance, isPending, error } = useInstance()
const releases = 'https://github.com/runsten-app/runsten/releases'
const updating = 'https://runsten.app/docs/self-hosting/updating'
</script>

<template>
  <div class="release-info">
    <v-alert v-if="error" type="error" variant="tonal">{{ t('release.error') }}</v-alert>
    <v-progress-linear v-else-if="isPending" indeterminate :aria-label="t('release.loading')" />
    <template v-else-if="instance">
      <p class="text-body-medium version">
        {{ t('release.version', { version: instance.version }) }}
      </p>
      <v-alert v-if="instance.update" type="info" variant="tonal" class="update mt-3">
        {{ t('release.update', { version: instance.update.version }) }}
        <div class="d-flex flex-wrap ga-2 mt-2">
          <v-btn
            :href="instance.update.url"
            target="_blank"
            rel="noreferrer"
            :append-icon="mdiOpenInNew"
            variant="outlined"
            color="info"
            size="small"
            class="notes"
          >
            {{ t('release.notes') }}
            <span class="d-sr-only">{{ t('release.newTab') }}</span>
          </v-btn>
          <v-btn
            :href="updating"
            target="_blank"
            rel="noreferrer"
            :append-icon="mdiOpenInNew"
            variant="outlined"
            color="info"
            size="small"
            class="how-to"
          >
            {{ t('release.howTo') }}
            <span class="d-sr-only">{{ t('release.newTab') }}</span>
          </v-btn>
        </div>
      </v-alert>
      <p v-else-if="instance.update_check" class="text-body-medium text-medium-emphasis latest">
        {{ t('release.latest') }}
      </p>
      <p v-else class="text-body-medium">
        <a :href="releases" target="_blank" rel="noreferrer" class="releases">
          {{ t('release.releases') }}<v-icon :icon="mdiOpenInNew" size="x-small" class="ms-1" />
          <span class="d-sr-only">{{ t('release.newTab') }}</span>
        </a>
      </p>
    </template>
  </div>
</template>
