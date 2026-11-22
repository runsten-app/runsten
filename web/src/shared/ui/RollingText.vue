<script setup lang="ts">
import { computed } from 'vue'

// RollingText is a formatted figure whose digits roll to their new value, as on an
// odometer, when it changes under the eyes (a state read again, during a charge or a
// trip); shown, it is drawn at once, a figure merely displayed having nothing to say by
// moving. The text stays as it was written: each character is its own span, a digit with a strip of the
// ten drawn over it by CSS (content, so that the text read and copied is the figure's
// alone). The characters are matched from the right, where the units are, so that
// 99 → 100 rolls the two nines and brings a 1 in. With reduced motion, nothing moves.
const props = defineProps<{ text: string }>()

const chars = computed(() => {
  const all = [...props.text]
  return all.map((c, i) => ({
    c,
    key: all.length - i,
    digit: c >= '0' && c <= '9' ? Number(c) : null,
  }))
})
</script>

<template>
  <span class="rolling"
    ><template v-for="ch in chars" :key="ch.key"
      ><span v-if="ch.digit !== null" class="roll" :style="{ '--digit': ch.digit }"
        ><span class="ghost">{{ ch.c }}</span></span
      ><template v-else>{{ ch.c }}</template></template
    ></span
  >
</template>

<style scoped>
.rolling {
  white-space: inherit;
}
/* The digit itself keeps the place, the baseline and the text, transparent; the strip
   is drawn above it, clipped to its line (clip-path, which leaves the baseline alone,
   where overflow would move it to the bottom). */
.roll {
  position: relative;
  display: inline-block;
  clip-path: inset(0 -0.1em);
}
.ghost {
  color: transparent;
}
.roll::before {
  content: '0\A 1\A 2\A 3\A 4\A 5\A 6\A 7\A 8\A 9';
  position: absolute;
  inset-block-start: 0;
  inset-inline: 0;
  text-align: center;
  white-space: pre;
  letter-spacing: 0;
  translate: 0 calc(var(--digit) * -10%);
}
@media (prefers-reduced-motion: no-preference) {
  .roll::before {
    transition: translate 0.9s var(--motion-spring);
  }
}
</style>
