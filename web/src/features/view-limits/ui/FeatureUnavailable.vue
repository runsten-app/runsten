<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { limitsPage } from '@/entities/session'

// FeatureUnavailable stands where a function the account's offer leaves out would be:
// what it is, and the page that lifts the limit, when the instance has one.
const props = defineProps<{ feature: 'costs' | 'stats' | 'mqtt' }>()
const { t } = useI18n()
const texts = {
  costs: () => t('limits.costs'),
  stats: () => t('limits.stats'),
  mqtt: () => t('limits.mqtt'),
}
const text = computed(() => texts[props.feature]())
</script>

<template>
  <v-alert type="info" variant="tonal" class="feature-unavailable">
    {{ text }}
    <v-btn
      v-if="limitsPage"
      :to="limitsPage"
      variant="outlined"
      color="info"
      size="small"
      class="mt-2 d-block"
    >
      {{ t('limits.lift') }}
    </v-btn>
  </v-alert>
</template>
