<script setup lang="ts">
import { nextTick, ref } from 'vue'

// ErrorSummary lists what stops a form from being sent, at its top. Once sent, the form
// focuses it: a screen reader reads it all, and each line leads to its field.
defineProps<{
  title: string
  items: readonly { field?: string; text: string }[]
}>()

const root = ref<HTMLElement | null>(null)

// focusField focuses a field by its ID without following the link: with the document's
// <base href>, "#id" would lead to another page. A field in a closed <details> opens it.
async function focusField(id: string) {
  const el = document.getElementById(id)
  const details = el?.closest('details')
  if (details && !details.open) {
    details.open = true
    await nextTick()
  }
  el?.focus()
  el?.scrollIntoView?.({ block: 'center' })
}

defineExpose({ focus: () => root.value?.focus() })
</script>

<template>
  <div ref="root" class="error-summary mb-4" tabindex="-1">
    <v-alert type="error" variant="tonal">
      <h2 class="text-title-medium mb-2">{{ title }}</h2>
      <ul class="ps-4">
        <li v-for="(item, i) in items" :key="i" class="text-body-medium">
          <a v-if="item.field" :href="`#${item.field}`" @click.prevent="focusField(item.field)">{{
            item.text
          }}</a>
          <template v-else>{{ item.text }}</template>
        </li>
      </ul>
    </v-alert>
  </div>
</template>

<style scoped>
.error-summary:focus-visible {
  outline: 2px solid rgb(var(--v-theme-error));
  outline-offset: 2px;
  border-radius: 4px;
}
.error-summary a {
  color: inherit;
}
</style>
