<script setup lang="ts">
// TileSkeleton stands for a view still loading: the shapes of its tiles, or of a list's
// rows, a light sweeping over them (the class skeleton, app/styles/base.css). For screen
// readers, a status named by its label, as the progress bar it replaces was.
withDefaults(defineProps<{ label: string; layout?: 'tiles' | 'rows' }>(), { layout: 'tiles' })
</script>

<template>
  <div class="tile-skeleton" :class="`tile-skeleton--${layout}`" role="status" :aria-label="label">
    <template v-if="layout === 'tiles'">
      <div class="skeleton headline" />
      <div class="skeleton hero" />
      <div class="pair">
        <div class="skeleton tile" />
        <div class="skeleton tile" />
      </div>
    </template>
    <template v-else>
      <div class="skeleton day" />
      <div class="rows">
        <div v-for="i in 4" :key="i" class="skeleton row" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.tile-skeleton {
  display: grid;
  gap: 12px;
}
.headline {
  inline-size: min(320px, 70%);
  block-size: 28px;
  border-radius: var(--v-control-radius);
}
.hero {
  block-size: 220px;
}
.pair {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 12px;
}
.tile {
  block-size: 180px;
}
.day {
  inline-size: 140px;
  block-size: 16px;
  border-radius: var(--v-control-radius);
}
.rows {
  display: grid;
  gap: 2px;
}
.row {
  block-size: 68px;
  border-radius: 0;
}
.row:first-child {
  border-start-start-radius: var(--v-tile-radius);
  border-start-end-radius: var(--v-tile-radius);
}
.row:last-child {
  border-end-start-radius: var(--v-tile-radius);
  border-end-end-radius: var(--v-tile-radius);
}
@media (max-width: 599.98px) {
  .hero {
    block-size: 180px;
  }
}
</style>
