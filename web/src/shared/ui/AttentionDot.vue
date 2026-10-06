<script setup lang="ts">
// AttentionDot marks what needs the user, as a small dot; screen readers read its label.
// Beside a control that has a name of its own, give the control aria-describedby its id.
defineProps<{ label: string }>()
</script>

<template>
  <span class="attention-dot"
    ><span class="d-sr-only">{{ label }}</span></span
  >
</template>

<style scoped>
.attention-dot {
  display: inline-block;
  inline-size: 8px;
  block-size: 8px;
  border-radius: 50%;
  background: rgb(var(--v-theme-error));
}
</style>
