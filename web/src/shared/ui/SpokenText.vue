<script setup lang="ts">
// SpokenText shows a text, and gives screen readers other words for it when it has them:
// a range of amounts is read "from €5.24 to €5.79", not "5.24 dash 5.79".
defineProps<{ text: string; spoken?: string }>()
</script>

<template>
  <span v-if="spoken && spoken !== text"
    ><span aria-hidden="true" class="shown">{{ text }}</span
    ><span class="d-sr-only">{{ spoken }}</span></span
  >
  <span v-else>{{ text }}</span>
</template>
