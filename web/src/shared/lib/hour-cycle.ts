import { readonly, ref } from 'vue'

// The clock the reader chose: the locale's (by default), or always 24 or 12 hours. A
// browser that lists "en-US" among its languages gives English 12-hour times, whatever
// the reader is used to. A preference, not a secret: localStorage is fine.
export const hourCycles = ['auto', 'h23', 'h12'] as const
export type HourCycle = (typeof hourCycles)[number]

const storageKey = 'runsten.clock'

function isHourCycle(s: string | null): s is HourCycle {
  return (hourCycles as readonly (string | null)[]).includes(s)
}

const stored = localStorage.getItem(storageKey)
const chosen = ref<HourCycle>(isHourCycle(stored) ? stored : 'auto')

export const hourCycle = readonly(chosen)

export function setHourCycle(c: HourCycle) {
  chosen.value = c
  localStorage.setItem(storageKey, c)
}

// withHourCycle adds the clock chosen to a locale, as its Unicode extension ("en-US" →
// "en-US-u-hc-h23"), which every Intl.DateTimeFormat made with it follows.
export function withHourCycle(locale: string, c: HourCycle): string {
  if (c === 'auto') return locale
  try {
    return new Intl.Locale(locale, { hourCycle: c }).toString()
  } catch {
    return locale
  }
}
