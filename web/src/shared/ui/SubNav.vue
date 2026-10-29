<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'

// SubNav leads to the areas of a settings page, each a route of its own: links in a <nav>,
// the one shown marked by the router (aria-current="page"), never a tablist, whose roving
// tabindex would leave them to the arrow keys. A column beside the content from md up,
// its active item a neutral pill; on a phone, one row under the title, which scrolls
// sideways rather than the page.
defineProps<{ label: string; items: { to: RouteLocationRaw; label: string }[] }>()
</script>

<template>
  <nav class="sub-nav" :aria-label="label">
    <ul>
      <li v-for="item in items" :key="item.label">
        <router-link :to="item.to" class="item text-body-large">{{ item.label }}</router-link>
      </li>
    </ul>
  </nav>
</template>

<style scoped>
ul {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.item {
  display: block;
  padding: 8px 14px;
  border-radius: var(--v-control-radius);
  color: rgb(var(--v-theme-text-secondary));
  text-decoration: none;
  white-space: nowrap;
}
.item:hover {
  color: rgb(var(--v-theme-on-background));
  background: rgba(var(--v-theme-on-background), 0.04);
}
.item[aria-current='page'] {
  font-weight: 500;
  color: rgb(var(--v-theme-on-surface-variant));
  background: rgb(var(--v-theme-surface-variant));
}
.item:focus-visible {
  outline: 2px solid rgb(var(--v-theme-primary));
  outline-offset: 2px;
}
@media (max-width: 959.98px) {
  .sub-nav {
    overflow-x: auto;
    /* The focus ring stays inside the scrolling row. */
    padding: 3px;
    margin: -3px;
  }
  ul {
    flex-direction: row;
  }
}
</style>
