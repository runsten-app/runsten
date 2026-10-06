<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { isLocale, localeNames, locales, setLocale } from '@/shared/i18n'

const { t, locale } = useI18n()
</script>

<template>
  <v-select
    :model-value="locale"
    :items="locales.map((l) => ({ value: l, title: localeNames[l] }))"
    :label="t('locale.label')"
    density="compact"
    hide-details
    class="locale"
    @update:model-value="(l: string) => isLocale(l) && setLocale(l)"
  />
</template>

<style scoped>
.locale {
  min-width: 9rem;
}
</style>
