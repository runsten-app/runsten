<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import { appearances, isAppearance, storeAppearance, type Appearance } from '@/shared/theme'

// AppearanceToggle picks the color scheme: the system's, or always light or dark. Kept in
// this browser; the focus stays on the button pressed.
const { t } = useI18n()
const theme = useTheme()
const current = computed<Appearance>(() => {
  const name = theme.name.value
  return theme.isSystem.value ? 'system' : isAppearance(name) ? name : 'system'
})
const labels = computed<Record<Appearance, string>>(() => ({
  system: t('preferences.appearance.system'),
  light: t('preferences.appearance.light'),
  dark: t('preferences.appearance.dark'),
}))

function choose(a: Appearance) {
  theme.change(a)
  storeAppearance(a)
}
</script>

<template>
  <v-btn-toggle
    :model-value="current"
    mandatory
    density="compact"
    class="segmented appearance-toggle"
    role="group"
    :aria-label="t('preferences.appearance.title')"
    @update:model-value="choose"
  >
    <v-btn v-for="a in appearances" :key="a" :value="a" :aria-pressed="a === current">
      {{ labels[a] }}
    </v-btn>
  </v-btn-toggle>
</template>
