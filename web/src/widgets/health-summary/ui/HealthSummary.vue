<script setup lang="ts">
import { mdiArrowRight } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { activeWarnings, needsAttention, tyresFine, tyreTrouble } from '@/entities/health'
import { useHealth, useHealthText } from '@/features/view-health'
import { StatusPill } from '@/shared/ui'

// HealthSummary tells on the vehicle's page whether its health needs attention, in three
// lines, and leads to its page. Hidden until the vehicle told anything of it, and when it
// cannot be read: the state stays whole.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const { health } = useHealth(() => props.vehicle)
const text = useHealthText()

const shown = computed(() => (health.value?.checked_at ? health.value : null))
const lines = computed(() => {
  const h = shown.value
  if (!h) return []
  const out: string[] = []
  // A tyre that warns, else OK only when the four are known: never fine for want of one.
  const [worst] = tyreTrouble(h)
  if (worst?.warning)
    out.push(
      t('health.tyres.summary.trouble', {
        wheel: text.wheels.value[worst.wheel],
        warning: text.tyreWarnings.value[worst.warning],
      }),
    )
  else if (tyresFine(h)) out.push(t('health.tyres.summary.ok'))
  if (h.service?.state)
    out.push(t('health.service.summary', { state: text.serviceStates.value[h.service.state] }))
  if (h.checks.length) {
    const active = activeWarnings(h).map((c) => text.checks.value[c.check])
    out.push(
      active.length
        ? t('health.warnings.summary', { list: active.join(', ') })
        : t('health.warnings.summaryNone'),
    )
  }
  return out
})
</script>

<template>
  <v-card v-if="shown" class="tile health-summary">
    <v-card-title tag="h2" class="d-flex align-center justify-space-between ga-2">
      {{ t('health.title') }}
      <StatusPill :tone="needsAttention(shown) ? 'warning' : 'success'">
        {{ needsAttention(shown) ? t('health.attention') : t('health.fine') }}
      </StatusPill>
    </v-card-title>
    <v-card-text>
      <ul class="lines">
        <li v-for="line in lines" :key="line">{{ line }}</li>
      </ul>
      <router-link :to="{ name: 'health', params: { vehicle } }" class="text-primary">
        {{ t('health.open') }}
        <v-icon :icon="mdiArrowRight" size="16" aria-hidden="true" />
      </router-link>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.lines {
  list-style: none;
  padding: 0;
  margin: 0 0 12px;
}
.lines li + li {
  margin-top: 4px;
}
</style>
