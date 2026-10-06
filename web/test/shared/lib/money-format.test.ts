import { describe, expect, it } from 'vitest'
import {
  amountInput,
  currencyUnit,
  formatAmount,
  formatAmounts,
  formatMajor,
  majorUnits,
  minorDigits,
  parseAmount,
} from '@/shared/lib'
import settings from '@fixtures/settings.json'

const plain = (s: string) => s.replace(/\s/g, ' ')
const eur = { code: 'EUR', minor_digits: 2 }
const isk = { code: 'ISK', minor_digits: 0 }

describe('formatAmount', () => {
  it.each([
    ['en-GB', eur, 524, '€5.24'],
    ['en-GB', eur, 0, '€0.00'],
    ['en-GB', eur, 5, '€0.05'],
    ['en-GB', eur, 123456789, '€1,234,567.89'],
    ['fr', eur, 524, '5,24 €'],
    ['sv', eur, 1240, '12,40 €'],
    ['sv', { code: 'SEK', minor_digits: 2 }, 1240, '12,40 kr'],
    ['en-GB', isk, 1240, 'ISK 1,240'],
    ['fr', isk, 0, '0 ISK'],
  ])('in %s, %o, %i', (locale, c, minor, want) =>
    expect(plain(formatAmount(minor, c, locale))).toBe(want),
  )

  // Only the division by 10^digits: the API has rounded min down and max up.
  it('never rounds again', () => {
    expect(majorUnits(579, 2)).toBe(5.79)
    expect(majorUnits(1240, 0)).toBe(1240)
    expect(plain(formatMajor(5.79, eur, 'en-GB'))).toBe('€5.79')
  })
})

describe('formatAmounts', () => {
  it.each([
    ['en-GB', '€5.24 – €5.79'],
    ['fr', '5,24–5,79 €'],
    ['sv', '5,24–5,79 €'],
  ])('writes a range as %s does', (locale, want) => {
    const a = formatAmounts({ min_minor: 524, max_minor: 579 }, eur, locale)
    expect(plain(a.text)).toBe(want)
    expect(plain(a.min)).toBe(plain(formatAmount(524, eur, locale)))
    expect(plain(a.max)).toBe(plain(formatAmount(579, eur, locale)))
  })

  // formatRange would write "~€8.50".
  it('writes equal bounds as one exact amount', () => {
    const a = formatAmounts({ min_minor: 850, max_minor: 850 }, eur, 'en-GB')
    expect(a).toEqual({ text: '€8.50', min: '€8.50', max: '€8.50' })
  })

  it('keeps a currency without decimals', () => {
    expect(plain(formatAmounts({ min_minor: 5, max_minor: 6 }, isk, 'en-GB').text)).toBe('ISK 5–6')
  })
})

describe('currency digits', () => {
  it('come from Intl, as the API gives them', () => {
    expect(minorDigits('EUR')).toBe(2)
    expect(minorDigits('ISK')).toBe(0)
    expect(currencyUnit('SEK')).toEqual({ code: 'SEK', minor_digits: 2 })
  })

  it.each(settings.currencies)('$code agrees with the API', (c) =>
    expect(minorDigits(c.code)).toBe(c.minor_digits),
  )
})

describe('parseAmount', () => {
  it.each([
    ['12,34', 2, 1234],
    ['12.3', 2, 1230],
    ['12', 2, 1200],
    ['12.', 2, 1200],
    [' 0 ', 2, 0],
    ['0.05', 2, 5],
    ['12.340', 2, 1234],
    ['10000000', 2, 1_000_000_000],
    ['1240', 0, 1240],
    ['1240.0', 0, 1240],
  ])('reads %s with %i decimals as %i', (s, digits, want) =>
    expect(parseAmount(s, digits)).toBe(want),
  )

  it.each([
    ['12.345', 2],
    ['12.5', 0],
    ['-1', 2],
    ['+1', 2],
    ['.5', 2],
    ['1e3', 2],
    ['', 2],
    ['abc', 2],
  ])('refuses %s with %i decimals', (s, digits) => expect(parseAmount(s, digits)).toBeNull())
})

describe('amountInput', () => {
  it.each([
    [850, 2, '8.50'],
    [5, 2, '0.05'],
    [0, 2, '0.00'],
    [1240, 0, '1240'],
  ])('writes %i with %i decimals as %s', (minor, digits, want) =>
    expect(amountInput(minor, digits)).toBe(want),
  )
})
