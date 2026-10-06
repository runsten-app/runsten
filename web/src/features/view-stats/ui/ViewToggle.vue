<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { statsViews, useStatsView, type StatsView } from '../composables/useStatsView'

// ViewToggle picks the charts of the statistics page: driving, charging or costs. The
// period, the split and the totals above stay the same for the three.
const { t } = useI18n()
const { view, setView } = useStatsView()
const labels = computed<Record<StatsView, string>>(() => ({
  driving: t('stats.view.driving'),
  charging: t('stats.view.charging'),
  costs: t('stats.view.costs'),
}))
</script>

<template>
  <v-btn-toggle
    :model-value="view"
    mandatory
    density="compact"
    class="segmented view-toggle"
    role="group"
    :aria-label="t('stats.view.label')"
    @update:model-value="(v: StatsView) => setView(v)"
  >
    <v-btn v-for="v in statsViews" :key="v" :value="v" :aria-pressed="v === view">
      {{ labels[v] }}
    </v-btn>
  </v-btn-toggle>
</template>
