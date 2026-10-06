<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { enableShortcuts, shortcutsEnabled } from '../model/enabled'

// ShortcutsToggle turns the single-key shortcuts on or off, in this browser; the focus
// stays on the button pressed.
const { t } = useI18n()
const choices = [true, false] as const
</script>

<template>
  <v-btn-toggle
    :model-value="shortcutsEnabled"
    mandatory
    density="compact"
    class="segmented shortcuts-toggle"
    role="group"
    :aria-label="t('shortcuts.title')"
    @update:model-value="enableShortcuts"
  >
    <v-btn
      v-for="on in choices"
      :key="String(on)"
      :value="on"
      :aria-pressed="on === shortcutsEnabled"
    >
      {{ on ? t('shortcuts.on') : t('shortcuts.off') }}
    </v-btn>
  </v-btn-toggle>
</template>
