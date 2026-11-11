<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ConnectionSettings, PleosBrand } from '@/entities/connection'
import { pleosBrands } from '@/entities/connection'
import PleosForm from './PleosForm.vue'

// PleosForms holds a section per brand of the Pleos Vehicle Data API — Hyundai, Kia,
// Genesis —, each with the owner's key of its cars: one key per brand, kept apart,
// told apart and removed apart.
// off: the source is turned off for the account. Only the brands with a key are
// shown, for it to be removed.
const props = defineProps<{ connection: ConnectionSettings; off?: boolean }>()
const shown = computed(() =>
  pleosBrands.filter((b) => !props.off || props.connection.pleos?.[b] != null),
)
const { t } = useI18n()

// The brands, written out: an interpolated key is invisible to the unused-key rule.
const names = computed<Record<PleosBrand, string>>(() => ({
  hyundai: t('pleos.brand.hyundai'),
  kia: t('pleos.brand.kia'),
  genesis: t('pleos.brand.genesis'),
}))
</script>

<template>
  <section
    v-for="brand of shown"
    :key="brand"
    :aria-labelledby="`connection-pleos-${brand}-title`"
    class="mb-8"
  >
    <h3 :id="`connection-pleos-${brand}-title`" class="text-title-small mb-3">
      {{ names[brand] }}
    </h3>
    <PleosForm :connection :brand :off />
  </section>
</template>
