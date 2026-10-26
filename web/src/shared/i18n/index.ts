import { ref } from 'vue'
import { createI18n } from 'vue-i18n'
import en from './locales/en.json'
import fr from './locales/fr.json'
import sv from './locales/sv.json'

export const locales = ['en', 'fr', 'sv'] as const
export type Locale = (typeof locales)[number]

// Each language is named in itself, whatever the current one.
export const localeNames: Record<Locale, string> = { en: 'English', fr: 'Français', sv: 'Svenska' }

// A preference, not a secret: localStorage is fine.
const storageKey = 'runsten.locale'

export function isLocale(s: string): s is Locale {
  return (locales as readonly string[]).includes(s)
}

// base is the language of a browser's tag ("sv-SE" → "sv").
const base = (tag: string) => tag.toLowerCase().split('-')[0] ?? ''

// initialLocale picks the stored choice, else the first browser language Runsten speaks
// ("sv-SE" → "sv"), else English.
export function initialLocale(stored: string | null, languages: readonly string[]): Locale {
  if (stored && isLocale(stored)) return stored
  for (const l of languages) {
    const b = base(l)
    if (isLocale(b)) return b
  }
  return 'en'
}

// intlLocale is the locale of dates and numbers for a language: the browser's own region
// of it when it has one ("en" with "en-GB" → 24-hour times), else the bare language. When
// the language is English only because no browser language is spoken (not chosen), dates
// and numbers take the region of the browser's first language, their words staying
// English ("nl-NL" or "nl" → "en-NL": 24-hour times, "1.234,5").
export function intlLocale(l: Locale, languages: readonly string[], chosen: boolean): string {
  const first = languages[0]
  if (!chosen && first && !languages.some((b) => isLocale(base(b)))) {
    const region = regionOf(first)
    if (region) return `${l}-${region}`
  }
  return languages.find((b) => base(b) === l) ?? l
}

// regionOf is a browser language's region, the likeliest when it names none ("nl" → "NL");
// undefined for a tag Intl does not read.
function regionOf(tag: string): string | undefined {
  try {
    return new Intl.Locale(tag).maximize().region
  } catch {
    return undefined
  }
}

const stored = localStorage.getItem(storageKey)

// localeChosen tells whether the language was chosen in the preferences, rather than
// taken from the browser's.
export const localeChosen = ref(stored !== null && isLocale(stored))

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale(stored, navigator.languages),
  fallbackLocale: 'en',
  messages: { en, fr, sv },
})

export function setLocale(l: Locale) {
  i18n.global.locale.value = l
  localeChosen.value = true
  localStorage.setItem(storageKey, l)
  document.documentElement.lang = l
}
