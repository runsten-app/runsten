<script setup lang="ts">
import { mdiDownload, mdiFolderZipOutline } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { exportPath, exportZipPath, isExportActive, type AccountExport } from '@/entities/session'
import { isApiError } from '@/shared/api'
import { useFormat, useNow } from '@/shared/lib'
import { useAccountExport } from '../composables/useAccountExport'

// AccountData downloads all the account's data, whatever its offer. The JSON document
// comes as it is; the ZIP archive is built in the background — an account that has
// outgrown a request is still had — and held a few days, until its expires_at, or
// until the next one is asked for. The slot adds buttons after both.
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const {
  accountExport: exportJob,
  error: readError,
  start,
  isPending: starting,
  startError,
  reset,
} = useAccountExport()

const active = computed(() => isExportActive(exportJob.value))
const waiting = computed(() => {
  const j: AccountExport | null | undefined = exportJob.value
  return j?.status === 'pending' && new Date(j.not_before).getTime() > now.value
})
const failed = computed(() => exportJob.value?.status === 'failed')
const held = computed(() => exportJob.value?.status === 'done')
const size = computed(() => {
  const b = exportJob.value?.bytes ?? 0
  if (b <= 0) return ''
  if (b < 1024 * 1024) return t('account.export.zip.kilobytes', { n: (b / 1024).toFixed(1) })
  return t('account.export.zip.megabytes', { n: (b / 1024 / 1024).toFixed(1) })
})
// Once built, the ended time is the row's; held asks for it never to be missing.
const builtAt = computed(() => exportJob.value?.ended_at || exportJob.value?.created_at || '')
const error = computed(() => {
  const e = readError.value ?? startError.value
  if (!e) return null
  if (isApiError(e, 'export_active')) return t('account.export.zip.error.active')
  if (isApiError(e, 'network')) return t('account.export.zip.error.unavailable')
  return t('account.export.zip.error.internal')
})

async function build() {
  reset()
  try {
    await start()
  } catch {
    // told of by the error summary
  }
}
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

    <div class="archive mt-6">
      <h4 class="text-title-medium mb-2">{{ t('account.export.zip.title') }}</h4>
      <p class="text-body-medium mb-4">{{ t('account.export.zip.help') }}</p>
      <div class="d-flex flex-wrap align-center ga-2">
        <template v-if="exportJob && active">
          <v-progress-circular
            indeterminate
            size="24"
            :aria-label="t('account.export.zip.progress')"
          />
          <p class="text-body-medium building">
            {{ t('account.export.zip.progress') }}
          </p>
          <p v-if="size" class="text-body-medium text-medium-emphasis written">
            {{ t('account.export.zip.written', { size }) }}
          </p>
          <p v-if="waiting" class="text-body-medium text-medium-emphasis waiting">
            {{ t('account.export.zip.waiting', { time: f.dateTime(exportJob.not_before, now) }) }}
          </p>
        </template>
        <template v-else-if="held">
          <v-btn
            :href="exportZipPath"
            download
            :prepend-icon="mdiDownload"
            variant="tonal"
            class="archive-download"
          >
            {{ t('account.export.zip.download', { size }) }}
          </v-btn>
          <span class="text-body-small text-medium-emphasis held-at">
            {{
              t('account.export.zip.held', {
                time: f.dateTime(builtAt, now),
                until: f.dateTime(exportJob?.expires_at ?? '', now),
              })
            }}
          </span>
          <v-btn variant="text" class="archive-start ms-2" @click="build">
            {{ t('account.export.zip.again') }}
          </v-btn>
        </template>
        <template v-else>
          <v-btn
            :prepend-icon="mdiFolderZipOutline"
            variant="tonal"
            :loading="starting"
            class="archive-start"
            @click="build"
          >
            {{ failed ? t('account.export.zip.again') : t('account.export.zip.build') }}
          </v-btn>
          <span v-if="failed" class="text-body-small text-error failure">
            {{ t('account.export.zip.failed') }}
          </span>
        </template>
      </div>
      <p v-if="error" role="alert" class="text-body-small text-error mt-2 error-message">
        {{ error }}
      </p>
    </div>
  </div>
</template>
