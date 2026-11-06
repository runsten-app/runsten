<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  bar,
  hasPressures,
  tyresFine,
  tyreTrouble,
  wheels,
  type Health,
  type TyreWarning,
} from '@/entities/health'
import { useHealthText } from '@/features/view-health'
import { useFormat, useNow } from '@/shared/lib'
import { StatusPill } from '@/shared/ui'

// TyresPart is the tyres at a glance, in the state's tile of the doors and windows: laid
// out as the wheels, front first, each its pressure, or its warning for a car that gives
// none (a Volvo); a warning other than OK beside it. A pill says whether they are fine or
// need attention, as the Health page does, which keeps their details: the recommended
// pressures, their pressures over time.
const props = defineProps<{ health: Health }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const text = useHealthText()

const pressures = computed(() => hasPressures(props.health))
const tyres = computed(() =>
  wheels.map((w) => props.health.tyres?.wheels.find((x) => x.wheel === w) ?? null),
)
// Fine only when the four tyres are known: a tyre unknown gives no pill.
const tone = computed(() =>
  tyreTrouble(props.health).length ? 'warning' : tyresFine(props.health) ? 'success' : null,
)
const warningTones: Record<TyreWarning, 'success' | 'warning' | 'error'> = {
  ok: 'success',
  high: 'warning',
  fault: 'warning',
  low: 'error',
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
      <StatusPill v-if="tone" :tone>
        {{ tone === 'success' ? t('health.fine') : t('health.attention') }}
      </StatusPill>
    </div>
    <dl class="wheels">
      <template v-for="(w, i) in tyres" :key="wheels[i]">
        <div v-if="w" class="wheel">
          <dt>{{ text.wheels.value[w.wheel] }}</dt>
          <dd v-if="pressures" class="pressure">
            {{ w.pressure_kpa === null ? f.unknown() : f.quantity(bar(w.pressure_kpa), 'bar') }}
          </dd>
          <dd v-if="w.warning && (!pressures || w.warning !== 'ok')">
            <StatusPill :tone="warningTones[w.warning]" class="warning">
              {{ text.tyreWarnings.value[w.warning] }}
            </StatusPill>
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
  margin-bottom: 8px;
}
.tyres-title {
  font-size: 0.875rem;
  font-weight: 600;
}
.wheels {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px 16px;
  margin: 0;
}
.wheel {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 4px 8px;
}
.wheel dt {
  flex-basis: 100%;
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
.wheel dd {
  margin: 0;
}
.pressure {
  font-size: 22px;
  font-weight: 300;
  font-variant-numeric: tabular-nums;
}
.measured {
  margin-top: 8px;
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
</style>
