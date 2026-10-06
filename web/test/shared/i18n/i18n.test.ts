import { describe, expect, it } from 'vitest'
import { i18n, initialLocale, intlLocale, setLocale } from '@/shared/i18n'
import en from '@/shared/i18n/locales/en.json'
import fr from '@/shared/i18n/locales/fr.json'
import sv from '@/shared/i18n/locales/sv.json'

function keys(o: object, prefix = ''): string[] {
  return Object.entries(o).flatMap(([k, v]) =>
    typeof v === 'object' && v !== null ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`],
  )
}

describe('initialLocale', () => {
  it.each([
    { stored: 'sv', languages: ['fr-FR'], want: 'sv' },
    { stored: 'de', languages: ['fr-FR', 'en'], want: 'fr' },
    { stored: null, languages: ['de-DE', 'sv-SE'], want: 'sv' },
    { stored: null, languages: ['FR'], want: 'fr' },
    { stored: null, languages: ['de-DE'], want: 'en' },
    { stored: null, languages: [], want: 'en' },
  ])('$stored $languages → $want', ({ stored, languages, want }) => {
    expect(initialLocale(stored, languages)).toBe(want)
  })
})

describe('intlLocale', () => {
  it.each([
    { locale: 'en', languages: ['en-GB', 'fr'], want: 'en-GB' },
    { locale: 'en', languages: ['fr-FR', 'en-US'], want: 'en-US' },
    { locale: 'fr', languages: ['en-GB'], want: 'fr' },
    { locale: 'sv', languages: ['SV-fi'], want: 'SV-fi' },
    { locale: 'sv', languages: [], want: 'sv' },
  ] as const)('$locale $languages → $want', ({ locale, languages, want }) => {
    expect(intlLocale(locale, languages)).toBe(want)
  })
})

describe('messages', () => {
  it('have the same keys in every language', () => {
    expect(keys(fr).sort()).toEqual(keys(en).sort())
    expect(keys(sv).sort()).toEqual(keys(en).sort())
  })
})

describe('setLocale', () => {
  it('switches, remembers and sets the document language', () => {
    setLocale('sv')
    expect(i18n.global.locale.value).toBe('sv')
    expect(localStorage.getItem('runsten.locale')).toBe('sv')
    expect(document.documentElement.lang).toBe('sv')
    setLocale('en')
  })
})
