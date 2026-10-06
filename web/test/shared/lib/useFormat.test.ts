import { afterEach, describe, expect, it } from 'vitest'
import { i18n, type Locale } from '@/shared/i18n'
import { useFormat } from '@/shared/lib'
import { withSetup } from '@test/utils'

const plain = (s: string) => s.replace(/\s/g, ' ')

afterEach(() => {
  i18n.global.locale.value = 'en'
})

describe('useFormat', () => {
  it.each<[Locale, string]>([
    ['en', 'Unknown'],
    ['fr', 'Inconnu'],
    ['sv', 'Okänt'],
  ])('shows a missing value as unknown in %s', (locale, unknown) => {
    i18n.global.locale.value = locale
    const { result: f } = withSetup(useFormat)
    expect(f.quantity(null, 'km')).toBe(unknown)
    expect(f.quantity(undefined, 'percent')).toBe(unknown)
    expect(f.coordinates(null)).toBe(unknown)
    expect(f.unknown()).toBe(unknown)
  })

  it('never shows an unknown as 0', () => {
    const { result: f } = withSetup(useFormat)
    expect(plain(f.quantity(0, 'km'))).toBe('0 km')
  })

  it('follows the chosen language', () => {
    const { result: f } = withSetup(useFormat)
    expect(plain(f.quantity(7.4, 'kW'))).toBe('7.4 kW')
    i18n.global.locale.value = 'fr'
    expect(plain(f.quantity(7.4, 'kW'))).toBe('7,4 kW')
    expect(f.coordinates({ lat: 45.764, lon: 4.8357 })).toBe('45.76400, 4.83570')
  })

  it('shows a change, each side unknown on its own', () => {
    const { result: f } = withSetup(useFormat)
    expect(plain(f.change(62, 54.5, 'percent'))).toBe('62% → 55%')
    expect(plain(f.change(null, 54, 'percent'))).toBe('Unknown → 54%')
    expect(f.change(null, null, 'km')).toBe('Unknown')
  })

  it('shows bounds and durations in the chosen language', () => {
    const { result: f } = withSetup(useFormat)
    const start = { after: '2026-09-28T06:55:00Z', before: '2026-09-28T07:01:00Z' }
    const end = { after: '2026-09-28T07:39:00Z', before: '2026-09-28T07:40:00Z' }
    expect(f.dayKey(start.after)).toBe('2026-09-28')
    expect(f.bounds(start, '2026-09-28')).toBe('06:55–07:01')
    expect(f.time(end.before, '2026-09-28')).toBe('07:40')
    expect(plain(f.duration(start, end))).toBe('38–45 mins')
    expect(f.day(start.after)).toBe('Monday, 28 September 2026')
    i18n.global.locale.value = 'fr'
    expect(plain(f.bounds(start, '2026-09-28'))).toBe('06:55 – 07:01')
    expect(plain(f.duration(start, end))).toBe('38–45 min')
  })

  it('shows prices, currencies, days and months in the chosen language', () => {
    const { result: f } = withSetup(useFormat)
    const eur = { code: 'EUR', minor_digits: 2 }
    expect(f.price(0.2142, eur)).toBe('€0.2142/kWh')
    expect(f.currencyName('EUR')).toBe('Euro')
    expect(f.currencySymbol('GBP')).toBe('£')
    expect(f.weekday(0)).toBe('Monday')
    expect(f.month(1, 'short')).toBe('Jan')
    expect(f.calendarDay('2026-08-01')).toBe('1 Aug 2026')
    expect(f.number(0.88)).toBe('0.88')
    // A count with at most the decimals asked: the cycles, one decimal.
    expect(f.number(14.4, 1)).toBe('14.4')
    expect(f.number(1.15, 1)).toBe('1.2')
    expect(f.number(38, 1)).toBe('38')
    // A deviation is signed, never clamped: the sign tells as much as the size.
    expect(f.signedPercent(3)).toBe('+3%')
    expect(f.signedPercent(-3)).toBe('-3%')
    expect(f.signedPercent(null)).toBe('Unknown')
    i18n.global.locale.value = 'fr'
    expect(plain(f.price(0.2142, eur))).toBe('0,2142 €/kWh')
    expect(f.currencyName('EUR')).toBe('euro')
    expect(f.weekday(6)).toBe('dimanche')
    expect(f.month(8)).toBe('août')
    expect(f.number(0.88)).toBe('0,88')
  })

  // A range is read as words by a screen reader: "5.24 dash 5.79" says nothing.
  it.each<[Locale, string, string, string]>([
    ['en', '€5.24 – €5.79', 'from €5.24 to €5.79', 'Free'],
    ['fr', '5,24–5,79 €', 'de 5,24 € à 5,79 €', 'Gratuit'],
    ['sv', '5,24–5,79 €', 'från 5,24 € till 5,79 €', 'Gratis'],
  ])('shows the bounds of a cost in %s, and what a screen reader says', (l, text, spoken, free) => {
    i18n.global.locale.value = l
    const { result: f } = withSetup(useFormat)
    const eur = { code: 'EUR', minor_digits: 2 }
    const range = f.amounts({ min_minor: 524, max_minor: 579 }, eur)
    expect([plain(range.text), plain(range.spoken)]).toEqual([text, spoken])
    const one = f.amounts({ min_minor: 850, max_minor: 850 }, eur)
    expect(one.spoken).toBe(one.text)
    expect(f.chargeCost({ currency: 'EUR', min_minor: 0, max_minor: 0 })).toEqual({
      text: free,
      spoken: free,
    })
  })

  it('shows an unknown cost as unknown, never 0, and a sum of 0 as an amount', () => {
    const { result: f } = withSetup(useFormat)
    const eur = { code: 'EUR', minor_digits: 2 }
    expect(f.amounts(null, eur)).toEqual({ text: 'Unknown', spoken: 'Unknown' })
    expect(f.amounts({ min_minor: 1, max_minor: 2 }, null)).toEqual({
      text: 'Unknown',
      spoken: 'Unknown',
    })
    expect(f.chargeCost(null)).toEqual({ text: 'Unknown', spoken: 'Unknown' })
    expect(f.amounts({ min_minor: 0, max_minor: 0 }, eur).text).toBe('€0.00')
    expect(f.amount(1240, eur)).toBe('€12.40')
    expect(f.majorAmount(12.4, eur)).toBe('€12.40')
    // A charge's cost names its currency by code: its digits come with it.
    expect(plain(f.chargeCost({ currency: 'ISK', min_minor: 1240, max_minor: 1240 }).text)).toBe(
      'ISK 1,240',
    )
  })
})
