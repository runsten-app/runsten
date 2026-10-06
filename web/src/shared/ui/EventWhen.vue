<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useFormat, type Bounds } from '@/shared/lib'

// EventWhen tells when an event happened: its start and end bounds ("06:55–07:01 →
// 07:39–07:40"), or, for a reconstructed one, the only interval known. Times on day (a
// list's heading) leave out their date.
const props = defineProps<{ start: Bounds; end: Bounds; reconstructed: boolean; day?: string }>()
const { t } = useI18n()
const f = useFormat()

const text = computed(() =>
  props.reconstructed
    ? t('event.between', {
        from: f.time(props.start.after, props.day),
        to: f.time(props.end.before, props.day),
      })
    : t('event.span', {
        start: f.bounds(props.start, props.day),
        end: f.bounds(props.end, props.day),
      }),
)
</script>

<template>
  <span class="event-when">{{ text }}</span>
</template>
