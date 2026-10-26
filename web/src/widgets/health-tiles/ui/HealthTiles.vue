<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  activeWarnings,
  bar,
  hasPressures,
  recommendedFor,
  tyreTrouble,
  type Health,
  type ServiceState,
  type TyreWarning,
} from '@/entities/health'
import { useHealthText } from '@/features/view-health'
import { useFormat, useNow } from '@/shared/lib'
import { StatusPill } from '@/shared/ui'

// HealthTiles shows the vehicle's latest health: its tyres, wheel by wheel, against the
// pressure it recommends; its next service; and its warnings, the active ones named, the
// others counted. What the vehicle does not report is said, never shown as fine.
const props = defineProps<{ health: Health }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const text = useHealthText()

const pressure = (kPa: number | null) => (kPa === null ? null : f.quantity(bar(kPa), 'bar'))
const pressures = computed(() => hasPressures(props.health))
const tyresTone = computed(() => (tyreTrouble(props.health).length ? 'warning' : 'success'))
const warningTones: Record<TyreWarning, 'success' | 'warning' | 'error'> = {
  ok: 'success',
  high: 'warning',
  fault: 'warning',
  low: 'error',
  very_low: 'error',
}
const serviceTones: Record<ServiceState, 'success' | 'warning' | 'error' | 'neutral'> = {
  ok: 'success',
  soon: 'neutral',
  due: 'warning',
  overdue: 'error',
}
const active = computed(() => activeWarnings(props.health))
const measured = (at: string | null) =>
  at ? t('health.reported', { time: f.dateTime(at, now.value) }) : null
const tyresAt = computed(
  () => props.health.tyres?.wheels.find((w) => w.reported_at)?.reported_at ?? null,
)
const checksAt = computed(() => props.health.checks.find((c) => c.reported_at)?.reported_at ?? null)
</script>

<template>
  <v-row density="compact" class="health-tiles">
    <v-col cols="12" md="6">
      <v-card class="tile h-100" data-test="tyres">
        <v-card-title tag="h2" class="d-flex align-center justify-space-between ga-2">
          {{ t('health.tyres.title') }}
          <StatusPill v-if="health.tyres" :tone="tyresTone">
            {{ tyresTone === 'success' ? t('health.fine') : t('health.attention') }}
          </StatusPill>
        </v-card-title>
        <v-card-text>
          <p v-if="!health.tyres" class="mb-0">{{ t('health.tyres.notReported') }}</p>
          <template v-else>
            <dl class="wheels">
              <div v-for="w in health.tyres.wheels" :key="w.wheel" class="wheel inset">
                <dt class="text-medium-emphasis">{{ text.wheels.value[w.wheel] }}</dt>
                <dd v-if="w.pressure_kpa !== null" class="figure">
                  {{ pressure(w.pressure_kpa) }}
                </dd>
                <dd v-if="w.warning">
                  <StatusPill :tone="warningTones[w.warning]">{{
                    text.tyreWarnings.value[w.warning]
                  }}</StatusPill>
                </dd>
                <dd
                  v-if="pressures && recommendedFor(health, w) !== null"
                  class="text-medium-emphasis"
                >
                  {{
                    w.wheel.startsWith('front')
                      ? t('health.tyres.recommendedFront')
                      : t('health.tyres.recommendedRear')
                  }}
                  {{ pressure(recommendedFor(health, w)) }}
                </dd>
              </div>
            </dl>
            <p v-if="!pressures" class="text-medium-emphasis mt-3 mb-0">
              {{ t('health.tyres.noPressure') }}
            </p>
            <p v-if="measured(tyresAt)" class="text-medium-emphasis footer mb-0">
              {{ measured(tyresAt) }}
            </p>
          </template>
        </v-card-text>
      </v-card>
    </v-col>

    <v-col cols="12" md="6">
      <v-card class="tile h-100" data-test="service">
        <v-card-title tag="h2" class="d-flex align-center justify-space-between ga-2">
          {{ t('health.service.title') }}
          <StatusPill v-if="health.service?.state" :tone="serviceTones[health.service.state]">
            {{ text.serviceStates.value[health.service.state] }}
          </StatusPill>
        </v-card-title>
        <v-card-text>
          <p v-if="!health.service" class="mb-0">{{ t('health.service.notReported') }}</p>
          <template v-else>
            <dl class="fields">
              <div>
                <dt class="text-medium-emphasis">{{ t('health.service.time') }}</dt>
                <dd>
                  <template v-if="health.service.days !== null">
                    {{
                      t(
                        'health.service.days',
                        { n: f.number(health.service.days) },
                        health.service.days,
                      )
                    }}
                  </template>
                  <template v-else-if="health.service.months !== null">
                    {{
                      t(
                        'health.service.months',
                        { n: f.number(health.service.months) },
                        health.service.months,
                      )
                    }}
                  </template>
                  <template v-else>{{ f.unknown() }}</template>
                </dd>
              </div>
              <div>
                <dt class="text-medium-emphasis">{{ t('health.service.distance') }}</dt>
                <dd>{{ f.quantity(health.service.distance_km, 'km') }}</dd>
              </div>
            </dl>
            <p v-if="measured(health.service.reported_at)" class="text-medium-emphasis footer mb-0">
              {{ measured(health.service.reported_at) }}
            </p>
          </template>
        </v-card-text>
      </v-card>
    </v-col>

    <v-col cols="12">
      <v-card class="tile" data-test="warnings">
        <v-card-title tag="h2" class="d-flex align-center justify-space-between ga-2">
          {{ t('health.warnings.title') }}
          <StatusPill v-if="health.checks.length" :tone="active.length ? 'warning' : 'success'">
            {{ active.length ? t('health.attention') : t('health.fine') }}
          </StatusPill>
        </v-card-title>
        <v-card-text>
          <p v-if="!health.checks.length" class="mb-0">{{ t('health.warnings.notReported') }}</p>
          <ul v-else-if="active.length" class="active">
            <li v-for="c in active" :key="c.check">{{ text.checks.value[c.check] }}</li>
          </ul>
          <p v-else class="mb-0">
            {{
              t('health.warnings.none', { n: f.number(health.checks.length) }, health.checks.length)
            }}
          </p>
          <p
            v-if="health.checks.length && measured(checksAt)"
            class="text-medium-emphasis footer mb-0"
          >
            {{ measured(checksAt) }}
          </p>
        </v-card-text>
      </v-card>
    </v-col>
  </v-row>
</template>

<style scoped>
.wheels {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
}
.wheel {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  padding: 12px;
  border-radius: var(--v-control-radius);
}
.wheel dd {
  margin: 0;
}
.inset {
  background: rgb(var(--v-theme-surface-variant));
}
.figure {
  font-size: 1.5rem;
  font-weight: 300;
}
.fields {
  display: grid;
  gap: 8px;
  margin: 0;
}
.fields dd {
  margin: 0;
}
.active {
  margin: 0;
  padding-inline-start: 20px;
}
.footer {
  margin-top: 12px;
  font-size: 0.8125rem;
}
</style>
