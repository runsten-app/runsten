<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { limitsPage } from '@/entities/session'
import { useFormat } from '@/shared/lib'
import { useLimits } from '../composables/useLimits'

// HistoryLimitNotice tells, above the trips, the charges and the statistics, from when
// the account's history is shown, when the instance limits it: the earlier events are
// kept, not deleted. Nothing without a limit. It leads to the page that lifts the limit,
// when the instance has one.
const { t } = useI18n()
const f = useFormat()
const { historyFrom: from } = useLimits()
</script>

<template>
  <v-alert v-if="from" type="info" variant="tonal" density="compact" class="history-limit mb-4">
    {{ t('history.limited', { day: f.day(from) }) }}
    <v-btn
      v-if="limitsPage"
      :to="limitsPage"
      variant="outlined"
      color="info"
      size="small"
      class="mt-2 d-block"
    >
      {{ t('history.lift') }}
    </v-btn>
  </v-alert>
</template>
