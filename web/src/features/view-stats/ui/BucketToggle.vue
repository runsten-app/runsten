<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { buckets, type Bucket, type Days } from '@/shared/lib'
import { useBucket } from '../composables/useBucket'

// BucketToggle picks the split of the charts: by day, week or month. A split that would
// give more intervals than the API allows is disabled.
const props = defineProps<{ days: Days }>()
const { t } = useI18n()
const { bucket, allowed, setBucket } = useBucket(() => props.days)
const labels = computed<Record<Bucket, string>>(() => ({
  day: t('stats.bucket.day'),
  week: t('stats.bucket.week'),
  month: t('stats.bucket.month'),
}))
</script>

<template>
  <v-btn-toggle
    :model-value="bucket"
    mandatory
    density="compact"
    class="segmented bucket-toggle"
    role="group"
    :aria-label="t('stats.bucket.label')"
    @update:model-value="(b: Bucket) => setBucket(b)"
  >
    <v-btn
      v-for="b in buckets"
      :key="b"
      :value="b"
      :disabled="!allowed.includes(b)"
      :aria-pressed="b === bucket"
    >
      {{ labels[b] }}
    </v-btn>
  </v-btn-toggle>
</template>
