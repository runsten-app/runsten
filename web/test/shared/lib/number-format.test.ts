import { describe, expect, it } from 'vitest'
import { formatQuantity, formatSignedPercent, kilowatts, type Unit } from '@/shared/lib'

// Intl writes no-break spaces (U+00A0, U+202F) in some locales and plain ones in others:
// the tables compare them as plain spaces.
const plain = (s: string | null) => s?.replace(/\s/g, ' ') ?? null

describe('formatQuantity', () => {
  it.each<[number | null, Unit, string, string | null]>([
    [64, 'percent', 'en-GB', '64%'],
    [64, 'percent', 'fr-FR', '64 %'],
    [64, 'percent', 'sv-SE', '64 %'],
    [12473, 'km', 'en-GB', '12,473 km'],
    [12473, 'km', 'fr-FR', '12 473 km'],
    [12473, 'km', 'sv-SE', '12 473 km'],
    [255.6, 'km', 'en-GB', '256 km'],
    [7.4, 'kW', 'en-GB', '7.4 kW'],
    [7.4, 'kW', 'fr-FR', '7,4 kW'],
    [7.4, 'kW', 'sv-SE', '7,4 kW'],
    [80, 'kWh', 'en-GB', '80 kWh'],
    [0, 'kW', 'en-GB', '0 kW'],
    [18.1, 'kWhPer100km', 'en-GB', '18.1 kWh/100 km'],
    [18.1, 'kWhPer100km', 'fr-FR', '18,1 kWh/100 km'],
    [null, 'km', 'en-GB', null],
    [null, 'percent', 'fr-FR', null],
  ])('%s %s in %s → %s', (value, unit, locale, want) => {
    expect(plain(formatQuantity(value, unit, locale))).toBe(want)
  })
})

describe('formatQuantity with digits', () => {
  it('keeps as many decimals as asked', () => {
    expect(plain(formatQuantity(32.2, 'km', 'en-GB'))).toBe('32 km')
    expect(plain(formatQuantity(32.2, 'km', 'en-GB', 1))).toBe('32.2 km')
  })
})

describe('kilowatts', () => {
  it('converts the API watts', () => expect(kilowatts(7400)).toBe(7.4))
})

describe('formatSignedPercent', () => {
  it.each<[number, string, string]>([
    [3, 'en-GB', '+3%'],
    [-3, 'en-GB', '-3%'],
    [0, 'en-GB', '+0%'],
    [-3, 'sv-SE', '−3 %'],
  ])('%s in %s → %s', (value, locale, want) => {
    expect(plain(formatSignedPercent(value, locale))).toBe(want)
  })
})
