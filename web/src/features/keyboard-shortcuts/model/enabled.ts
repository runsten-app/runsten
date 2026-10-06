import { readonly, ref } from 'vue'

// Single-key shortcuts can be turned off (WCAG 2.1.4): a speech input types letters and
// digits as words, and a stray key would change the page. On by default; a preference,
// not a secret: localStorage is fine.
const storageKey = 'runsten.shortcuts'
const enabled = ref(localStorage.getItem(storageKey) !== 'off')

export const shortcutsEnabled = readonly(enabled)

export function enableShortcuts(on: boolean) {
  enabled.value = on
  localStorage.setItem(storageKey, on ? 'on' : 'off')
}
