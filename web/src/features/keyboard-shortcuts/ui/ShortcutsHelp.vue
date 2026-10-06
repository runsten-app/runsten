<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

// ShortcutsHelp lists the keys, "?" opens it: the sections of the vehicle shown (none
// without one), then those of every page.
const props = defineProps<{ sections: string[] }>()
const open = defineModel<boolean>({ required: true })
const { t } = useI18n()

const rows = computed(() => [
  ...props.sections.map((label, i) => ({ key: String(i + 1), label })),
  { key: ',', label: t('settings.open') },
  { key: '?', label: t('shortcuts.help') },
  { key: t('shortcuts.shift'), label: t('shortcuts.hints') },
])
</script>

<template>
  <v-dialog v-model="open" max-width="28rem" aria-labelledby="shortcuts-title">
    <v-card class="shortcuts-help">
      <v-card-title id="shortcuts-title" tag="h2" class="text-title-large text-wrap">
        {{ t('shortcuts.title') }}
      </v-card-title>
      <v-card-text>
        <dl class="rows">
          <div v-for="row in rows" :key="row.key" class="row">
            <dt>
              <kbd>{{ row.key }}</kbd>
            </dt>
            <dd class="text-body-medium">{{ row.label }}</dd>
          </div>
        </dl>
        <p class="text-body-medium text-medium-emphasis mt-4">
          {{ t('shortcuts.turnOff') }}
          <router-link :to="{ name: 'preferences' }" @click="open = false">
            {{ t('preferences.title') }}
          </router-link>
        </p>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" class="close" @click="open = false">{{ t('shortcuts.close') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
/* One column of keys, as wide as the widest. */
.rows {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 8px 16px;
  align-items: center;
}
.row {
  display: grid;
  grid-template-columns: subgrid;
  grid-column: span 2;
  align-items: center;
}
.row dd {
  margin: 0;
}
kbd {
  display: inline-block;
  min-inline-size: 1.75rem;
  padding: 2px 8px;
  border-radius: var(--v-control-radius);
  background: rgb(var(--v-theme-surface-variant));
  color: rgb(var(--v-theme-on-surface));
  font-family: inherit;
  font-weight: 500;
  text-align: center;
}
</style>
