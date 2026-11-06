import { describe, expect, it } from 'vitest'
import { i18n, initialLocale, intlLocale, localeChosen, setLocale } from '@/shared/i18n'
import de from '@/shared/i18n/locales/de.json'
import en from '@/shared/i18n/locales/en.json'
import fr from '@/shared/i18n/locales/fr.json'
import it_ from '@/shared/i18n/locales/it.json'
import nb from '@/shared/i18n/locales/nb.json'
import nl from '@/shared/i18n/locales/nl.json'
import sv from '@/shared/i18n/locales/sv.json'

function keys(o: object, prefix = ''): string[] {
  return Object.entries(o).flatMap(([k, v]) =>
    typeof v === 'object' && v !== null ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`],
  )
}

describe('initialLocale', () => {
  it.each([
    { stored: 'sv', languages: ['fr-FR'], want: 'sv' },
    { stored: 'pt', languages: ['fr-FR', 'en'], want: 'fr' },
    { stored: null, languages: ['pt-PT', 'sv-SE'], want: 'sv' },
    { stored: null, languages: ['FR'], want: 'fr' },
    { stored: null, languages: ['it-CH', 'en'], want: 'it' },
    { stored: null, languages: ['nl-BE', 'fr-BE'], want: 'nl' },
    { stored: null, languages: ['pt-PT'], want: 'en' },
    // Norwegian, as browsers say it.
    { stored: null, languages: ['nb-NO'], want: 'nb' },
    { stored: null, languages: ['pt-PT', 'no-NO'], want: 'nb' },
    { stored: null, languages: ['nn'], want: 'nb' },
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
    { locale: 'nb', languages: ['nb-NO'], want: 'nb-NO' },
    // Norwegian said "no", or Nynorsk: Bokmål, in the browser's region.
    { locale: 'nb', languages: ['no-NO'], want: 'nb-NO' },
    { locale: 'nb', languages: ['no-US'], want: 'nb-US' },
    { locale: 'nb', languages: ['nn'], want: 'nb' },
    { locale: 'nb', languages: ['no-US', 'nb-NO'], want: 'nb-US' },
    { locale: 'en', languages: [], want: 'en' },
    // English as a fallback: the region of the browser's first language.
    { locale: 'en', languages: ['pl-PL'], want: 'en-PL' },
    { locale: 'en', languages: ['pt-BR', 'pl-PL'], want: 'en-BR' },
    { locale: 'en', languages: ['pl'], want: 'en-PL' },
    { locale: 'en', languages: ['x-invalid!'], want: 'en' },
    // A language spoken among the browser's: today's rule.
    { locale: 'en', languages: ['pl-PL', 'en-US'], want: 'en-US' },
  ] as const)('$locale $languages → $want', ({ locale, languages, want }) => {
    expect(intlLocale(locale, languages, false)).toBe(want)
  })

  it.each([
    { locale: 'en', languages: ['pl-PL'], want: 'en' },
    { locale: 'fr', languages: ['pl-PL'], want: 'fr' },
    { locale: 'en', languages: ['pl-PL', 'en-GB'], want: 'en-GB' },
  ] as const)('chosen $locale $languages → $want', ({ locale, languages, want }) => {
    expect(intlLocale(locale, languages, true)).toBe(want)
  })
})

describe('messages', () => {
  it('have the same keys in every language', () => {
    expect(keys(fr).sort()).toEqual(keys(en).sort())
    expect(keys(it_).sort()).toEqual(keys(en).sort())
    expect(keys(nb).sort()).toEqual(keys(en).sort())
    expect(keys(nl).sort()).toEqual(keys(en).sort())
    expect(keys(sv).sort()).toEqual(keys(en).sort())
    expect(keys(de).sort()).toEqual(keys(en).sort())
  })
})

describe('setLocale', () => {
  it('switches, remembers and sets the document language', () => {
    setLocale('sv')
    expect(i18n.global.locale.value).toBe('sv')
    expect(localStorage.getItem('runsten.locale')).toBe('sv')
    expect(document.documentElement.lang).toBe('sv')
    expect(localeChosen.value).toBe(true)
    setLocale('en')
  })
})
