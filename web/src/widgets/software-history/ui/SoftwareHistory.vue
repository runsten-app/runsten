<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SoftwareVersion } from '@/entities/health'
import { useSoftwareVersions } from '@/features/view-health'
import { useFormat } from '@/shared/lib'

// SoftwareHistory lists the versions of the vehicle's software, newest first, each with
// when the vehicle took it: between the last reading of the previous version and its own
// first one, as an event's bounds. Owners tie a change of range or consumption to an
// update with it. A vehicle whose source tells no version (a Volvo, a Polestar) shows
// nothing. The latest few only, the others on demand: the list grows with every update.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const { versions, error, hasMore, loadMore, isLoadingMore } = useSoftwareVersions(
  () => props.vehicle,
)

const few = 3
const all = ref(false)
const shown = computed(() => (all.value ? versions.value : versions.value.slice(0, few)))
const folded = computed(() => !all.value && versions.value.length > few)

function installed(v: SoftwareVersion): string {
  const { after, before } = v.installed
  return after
    ? t('software.installed', { when: f.bounds({ after, before }) })
    : t('software.firstRead', { when: f.time(before) })
}
</script>

<template>
  <v-card v-if="versions.length || error" class="software-history">
    <v-card-title tag="h2">{{ t('software.title') }}</v-card-title>
    <v-card-text>
      <v-alert v-if="error" type="error" variant="tonal" :class="{ 'mb-3': versions.length }">
        {{ t('software.error') }}
      </v-alert>
      <ol v-if="versions.length" class="versions">
        <li v-for="(v, i) in shown" :key="v.id" class="version">
          <!-- The spaces are text: the words stay apart once the spans are read out. -->
          <span class="name">{{ v.version }}</span
          >{{ ' ' }}
          <span v-if="i === 0" class="current">{{ t('software.current') }}{{ ' ' }}</span>
          <span class="when d-block">{{ installed(v) }}</span>
        </li>
      </ol>
      <v-btn v-if="folded" variant="tonal" class="all mt-3" @click="all = true">
        {{ t('software.all') }}
      </v-btn>
      <v-btn
        v-else-if="hasMore"
        variant="tonal"
        class="more mt-3"
        :loading="isLoadingMore"
        @click="loadMore()"
      >
        {{ t('software.more') }}
      </v-btn>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.versions {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.name {
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.current {
  margin-inline-start: 8px;
  font-size: 0.75rem;
  color: rgb(var(--v-theme-text-secondary));
}
.when {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
</style>
