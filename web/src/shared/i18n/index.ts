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

// initialLocale picks the stored choice, else the first browser language Runsten speaks
// ("sv-SE" → "sv"), else English.
export function initialLocale(stored: string | null, languages: readonly string[]): Locale {
  if (stored && isLocale(stored)) return stored
  for (const l of languages) {
    const base = l.toLowerCase().split('-')[0] ?? ''
    if (isLocale(base)) return base
  }
  return 'en'
}

// intlLocale is the locale of dates and numbers for a language: the browser's own region
// of it when it has one ("en" with "en-GB" → 24-hour times), else the bare language.
export function intlLocale(l: Locale, languages: readonly string[]): string {
  return languages.find((b) => b.toLowerCase().split('-')[0] === l) ?? l
}

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale(localStorage.getItem(storageKey), navigator.languages),
  fallbackLocale: 'en',
  messages: { en, fr, sv },
})

export function setLocale(l: Locale) {
  i18n.global.locale.value = l
  localStorage.setItem(storageKey, l)
  document.documentElement.lang = l
}
