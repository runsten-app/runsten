import { describe, expect, it } from 'vitest'
import { currencyName, currencySymbol, formatPricePerKwh } from '@/shared/lib'
import settings from '@fixtures/settings.json'

const plain = (s: string) => s.replace(/\s/g, ' ')
const eur = { code: 'EUR', minor_digits: 2 }

describe('formatPricePerKwh', () => {
  it.each([
    ['en-GB', 0.2142, '€0.2142/kWh'],
    ['en-GB', 0.2, '€0.20/kWh'],
    ['en-GB', 0, '€0.00/kWh'],
    ['en-GB', 0.123456, '€0.12346/kWh'],
    ['fr', 0.2142, '0,2142 €/kWh'],
    ['sv', 0.1589, '0,1589 €/kWh'],
  ])('in %s, %f', (locale, price, want) =>
    expect(plain(formatPricePerKwh(price, eur, locale))).toBe(want),
  )

  it('keeps the decimals of a currency without a minor unit, and more for a price', () => {
    const isk = { code: 'ISK', minor_digits: 0 }
    expect(plain(formatPricePerKwh(25, isk, 'en-GB'))).toBe('ISK 25/kWh')
    expect(plain(formatPricePerKwh(25.5, isk, 'en-GB'))).toBe('ISK 25.5/kWh')
  })

  it('is a number without a currency', () => {
    expect(formatPricePerKwh(0.2142, null, 'fr')).toBe('0,2142/kWh')
  })
})

describe('currencies', () => {
  it('are named and written in the language', () => {
    expect(currencyName('EUR', 'en-GB')).toBe('Euro')
    expect(currencyName('SEK', 'fr')).toBe('couronne suédoise')
    expect(currencyName('GBP', 'sv')).toBe('brittiskt pund')
    expect(currencySymbol('EUR', 'fr')).toBe('€')
    expect(currencySymbol('SEK', 'sv')).toBe('kr')
  })

  // The API gives each currency's digits (ISO 4217); Intl formats an amount with the
  // CLDR's. They must agree, or a cost would show a wrong number of decimals.
  it.each(settings.currencies)('$code has the digits Intl gives it', (c) => {
    const digits = new Intl.NumberFormat('en', { style: 'currency', currency: c.code })
    expect(digits.resolvedOptions().maximumFractionDigits).toBe(c.minor_digits)
  })
})
