<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { versionRuns, type VersionRun } from '@/entities/health'
import { useSoftwareVersions } from '@/features/view-health'
import { useFormat, useNow } from '@/shared/lib'
import { SpokenText } from '@/shared/ui'

// SoftwareVersions is the history of the vehicle's software, newest first: each version
// with when the vehicle took it (bounds, as an event's), how long it ran it, until the
// next one or until now, and how long its installation took when a reading saw it
// installing; a dash otherwise, never a duration of 0. Owners tie a change of range or
// consumption to an update with it. Its pages are read on demand.
const props = defineProps<{ vehicle: string }>()
const { t } = useI18n()
const f = useFormat()
const now = useNow(60_000)
const { versions, isPending, error, hasMore, loadMore, isLoadingMore } = useSoftwareVersions(
  () => props.vehicle,
)
const runs = computed(() => versionRuns(versions.value, now.value))
const unseen = computed(() => runs.value.some((r) => !r.install))

function installed(r: VersionRun): string {
  const { after, before } = r.version.installed
  return after
    ? t('software.installed', { when: f.bounds({ after, before }) })
    : t('software.firstRead', { when: f.time(before) })
}

// The current version's run goes on: said under its duration, as a second measure.
const ran = (r: VersionRun) => f.days(r.ran)
</script>

<template>
  <v-progress-linear v-if="isPending" indeterminate :aria-label="t('software.loading')" />
  <v-alert v-else-if="error && !versions.length" type="error" variant="tonal">
    {{ t('software.error') }}
  </v-alert>
  <p v-else-if="!versions.length" class="none">{{ t('software.none') }}</p>
  <v-card v-else class="software-versions">
    <v-card-title tag="h2">{{ t('software.history') }}</v-card-title>
    <v-card-text>
      <table class="versions">
        <thead>
          <tr>
            <th scope="col">{{ t('software.version') }}</th>
            <th scope="col" class="num">{{ t('software.ran') }}</th>
            <th scope="col" class="num">{{ t('software.installation') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in runs" :key="r.version.id" class="version">
            <th scope="row">
              <!-- The spaces are text: the words stay apart once the spans are read out. -->
              <span class="name">{{ r.version.version }}</span
              >{{ ' ' }}
              <span v-if="r.current" class="current">{{ t('software.running') }}{{ ' ' }}</span>
              <span class="when d-block">{{ installed(r) }}</span>
            </th>
            <td class="num ran">
              {{ ran(r) }}
              <span v-if="r.current" class="when d-block">{{ t('software.soFar') }}</span>
            </td>
            <td class="num install">
              <template v-if="r.install">{{ f.duration(r.install.start, r.install.end) }}</template>
              <SpokenText v-else class="unseen" text="—" :spoken="t('software.notSeen')" />
            </td>
          </tr>
        </tbody>
      </table>
      <v-alert v-if="error" type="error" variant="tonal" class="mt-3">
        {{ t('software.error') }}
      </v-alert>
      <p v-if="unseen" class="note mt-3 mb-0">{{ t('software.unseen') }}</p>
      <v-btn
        v-if="hasMore"
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
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  text-align: start;
  vertical-align: top;
  padding: 10px 16px 10px 0;
}
th:last-child,
td:last-child {
  padding-inline-end: 0;
}
thead th {
  font-size: 0.8125rem;
  font-weight: 400;
  color: rgb(var(--v-theme-text-secondary));
  padding-top: 0;
}
tbody th {
  font-weight: 400;
}
tbody tr + tr > * {
  border-top: 1px solid rgb(var(--v-theme-line));
}
.num {
  text-align: end;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
/* On a phone, a range of hours and minutes wraps rather than push its column out. */
@media (max-width: 599.98px) {
  .num {
    white-space: normal;
  }
}
.name {
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.current {
  white-space: nowrap;
  margin-inline-start: 8px;
  font-size: 0.75rem;
  color: rgb(var(--v-theme-text-secondary));
}
.when,
.note,
.unseen {
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-text-secondary));
}
</style>
