// The color scheme the reader chose: the system's (by default), or always light or dark.
// Vuetify names them so: its "system" theme follows prefers-color-scheme. A preference,
// not a secret: localStorage is fine.
export const appearances = ['system', 'light', 'dark'] as const
export type Appearance = (typeof appearances)[number]

const storageKey = 'runsten.appearance'

export function isAppearance(s: string | null): s is Appearance {
  return (appearances as readonly (string | null)[]).includes(s)
}

export function storedAppearance(): Appearance {
  const s = localStorage.getItem(storageKey)
  return isAppearance(s) ? s : 'system'
}

export function storeAppearance(a: Appearance) {
  localStorage.setItem(storageKey, a)
}
