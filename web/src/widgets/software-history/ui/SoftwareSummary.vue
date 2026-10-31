<script setup lang="ts">
import { mdiArrowRight } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSoftwareVersions } from '@/features/view-health'
import { useUpdateText, useVehicleState } from '@/features/watch-vehicle-state'
import { useFormat, useNow } from '@/shared/lib'
import { TileFigure } from '@/shared/ui'

// SoftwareSummary tells the version of the vehicle's software running, since when, and
// the update waiting for it; on the health page, it leads to the history of the versions
// (linked), titled as the page it leads to; on that page, under its title, it is the
// present. A vehicle whose source tells no version (a Volvo, a Polestar) shows nothing.
const props = withDefaults(defineProps<{ vehicle: string; linked?: boolean }>(), {
  linked: true,
})
const { t } = useI18n()
const f = useFormat()
const now = useNow()
const { state } = useVehicleState(() => props.vehicle)
const { versions } = useSoftwareVersions(() => props.vehicle)
const updateText = useUpdateText()

const current = computed(() => {
  const name = state.value?.software?.value ?? versions.value[0]?.version
  if (!name) return null
  // Its installation as derived, once the history has caught up with the state.
  const v = versions.value[0]?.version === name ? versions.value[0] : undefined
  if (!v) return { name, when: null }
  const { after, before } = v.installed
  return {
    name,
    when: after
      ? t('software.installed', { when: f.bounds({ after, before }) })
      : t('software.firstRead', { when: f.time(before) }),
  }
})
const update = computed(() => {
  const u = state.value?.software_update
  return u ? updateText(u.value, now.value) : null
})
</script>

<template>
  <v-card v-if="current" class="tile software-summary">
    <v-card-title tag="h2">{{ linked ? t('software.title') : t('software.now') }}</v-card-title>
    <v-card-text>
      <dl class="figures">
        <div>
          <dt class="text-medium-emphasis">{{ t('software.current') }}</dt>
          <dd>
            <TileFigure class="value" :text="current.name" size="kpi" />
            <span v-if="current.when" class="when d-block text-medium-emphasis">{{
              current.when
            }}</span>
          </dd>
        </div>
        <div v-if="update" class="update">
          <dt class="text-medium-emphasis">{{ t('state.update.title') }}</dt>
          <dd>{{ update }}</dd>
        </div>
      </dl>
      <router-link
        v-if="linked"
        :to="{ name: 'software', params: { vehicle } }"
        class="open text-primary"
      >
        {{ t('software.open') }}
        <v-icon :icon="mdiArrowRight" size="16" aria-hidden="true" />
      </router-link>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.figures {
  display: flex;
  flex-wrap: wrap;
  gap: 16px 48px;
}
dl,
dd {
  margin: 0;
}
dt {
  font-size: 0.8125rem;
  margin-bottom: 6px;
}
.when {
  font-size: 0.8125rem;
  margin-top: 4px;
}
.open {
  display: inline-block;
  margin-top: 12px;
}
.update dd {
  font-variant-numeric: tabular-nums;
}
</style>
