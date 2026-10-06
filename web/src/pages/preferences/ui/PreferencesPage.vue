<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ShortcutsToggle } from '@/features/keyboard-shortcuts'
import { AppearanceToggle } from '@/features/switch-appearance'
import { MapsToggle } from '@/features/switch-maps'
import { LocaleSelect } from '@/features/switch-locale'
import { mapTiles } from '@/shared/lib'
import { AppShell } from '@/widgets/app-shell'

// PreferencesPage is the user's own, kept in this browser: neither the account's settings
// nor a vehicle's.
const { t } = useI18n()
// Only where the instance has maps.
const maps = mapTiles() !== null
</script>

<template>
  <AppShell>
    <h1 class="text-headline-small mb-2">{{ t('preferences.title') }}</h1>
    <p class="text-body-medium mb-6">{{ t('preferences.intro') }}</p>
    <section aria-labelledby="preferences-language-title">
      <h2 id="preferences-language-title" class="text-title-large mb-3">
        {{ t('preferences.language') }}
      </h2>
      <LocaleSelect class="language" />
    </section>
    <section aria-labelledby="preferences-appearance-title" class="mt-8">
      <h2 id="preferences-appearance-title" class="text-title-large mb-1">
        {{ t('preferences.appearance.title') }}
      </h2>
      <p class="text-body-medium text-medium-emphasis mb-3">
        {{ t('preferences.appearance.hint') }}
      </p>
      <AppearanceToggle />
    </section>
    <section v-if="maps" aria-labelledby="preferences-maps-title" class="mt-8">
      <h2 id="preferences-maps-title" class="text-title-large mb-1">
        {{ t('preferences.maps.title') }}
      </h2>
      <p class="text-body-medium text-medium-emphasis mb-3">{{ t('preferences.maps.hint') }}</p>
      <MapsToggle />
    </section>
    <section aria-labelledby="preferences-shortcuts-title" class="mt-8">
      <h2 id="preferences-shortcuts-title" class="text-title-large mb-1">
        {{ t('shortcuts.title') }}
      </h2>
      <p class="text-body-medium text-medium-emphasis mb-3">{{ t('shortcuts.hint') }}</p>
      <ShortcutsToggle />
    </section>
  </AppShell>
</template>

<style scoped>
.language {
  max-width: 20rem;
}
</style>
