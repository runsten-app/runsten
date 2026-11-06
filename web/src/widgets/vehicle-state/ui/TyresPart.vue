<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  bar,
  hasPressures,
  tyreTrouble,
  wheels,
  type Health,
  type TyreWarning,
} from '@/entities/health'
import { useHealthText } from '@/features/view-health'
import { useFormat, useNow } from '@/shared/lib'
import { StatusPill } from '@/shared/ui'

// TyresPart is the tyres at a glance, in the state's tile of the doors and windows, as
// compact as it can: laid out as the wheels, front first, a line each, its pressure in
// bar where the car gives it (a Tesla, a Polestar), its warning otherwise (a Volvo). A
// warning other than OK colors the tyre and is said beside it, and a pill says that they
// need attention; nothing is said of tyres that are fine. The Health page keeps their
// details: the recommended pressures, their pressures over time.
const props = defineProps<{ health: Health }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const text = useHealthText()

const pressures = computed(() => hasPressures(props.health))
const tyres = computed(() =>
  wheels.map((w) => props.health.tyres?.wheels.find((x) => x.wheel === w) ?? null),
)
const attention = computed(() => tyreTrouble(props.health).length > 0)
const warningTones: Record<TyreWarning, 'success' | 'warning' | 'error'> = {
  ok: 'success',
  high: 'warning',
  fault: 'warning',
  low: 'warning',
  very_low: 'error',
}
const at = computed(
  () => props.health.tyres?.wheels.find((w) => w.reported_at)?.reported_at ?? null,
)
</script>

<template>
  <section class="tyres" data-test="state-tyres">
    <div class="tyres-head">
      <h3 class="tyres-title">{{ t('health.tyres.title') }}</h3>
      <StatusPill v-if="attention" tone="warning">{{ t('health.attention') }}</StatusPill>
    </div>
    <dl class="wheels">
      <template v-for="(w, i) in tyres" :key="wheels[i]">
        <div
          v-if="w"
          class="wheel"
          :class="w.warning && w.warning !== 'ok' ? `text-${warningTones[w.warning]}` : null"
        >
          <dt>{{ text.wheels.value[w.wheel] }}</dt>
          <dd>
            <span v-if="pressures" class="pressure">{{
              w.pressure_kpa === null ? f.unknown() : f.quantity(bar(w.pressure_kpa), 'bar')
            }}</span>
            <span v-if="w.warning && (!pressures || w.warning !== 'ok')" class="warning">{{
              text.tyreWarnings.value[w.warning]
            }}</span>
          </dd>
        </div>
        <div v-else class="wheel" />
      </template>
    </dl>
    <p v-if="at" class="measured mb-0">
      {{ t('health.reported', { time: f.dateTime(at, now) }) }}
    </p>
  </section>
</template>

<style scoped>
.tyres-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-block-size: 28px;
  margin-bottom: 4px;
}
.tyres-title {
  font-size: 0.875rem;
  font-weight: 600;
}
/* Two columns, front above rear: the wheels as seen from above. */
.wheels {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px 24px;
  margin: 0;
}
.wheel {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}
.wheel dt {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
.wheel dd {
  margin: 0;
  display: flex;
  align-items: baseline;
  gap: 6px;
  white-space: nowrap;
}
.pressure {
  font-size: 1.125rem;
  font-variant-numeric: tabular-nums;
}
.warning {
  font-size: 0.8125rem;
  font-weight: 500;
}
.measured {
  margin-top: 6px;
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
</style>
