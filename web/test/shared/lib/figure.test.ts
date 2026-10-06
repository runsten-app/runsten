import { describe, expect, it } from 'vitest'
import { figureParts, formatQuantity } from '@/shared/lib'

describe('figureParts', () => {
  it.each<[string, { value: string; unit: string } | null]>([
    ['80%', { value: '80', unit: '%' }],
    ['301 km', { value: '301', unit: ' km' }],
    ['13,119 km', { value: '13,119', unit: ' km' }],
    ['20.7 kWh/100 km', { value: '20.7', unit: ' kWh/100 km' }],
    ['0.4% per day', { value: '0.4', unit: '% per day' }],
    ['+3%', { value: '+3', unit: '%' }],
    ['−3 %', { value: '−3', unit: ' %' }],
    ['2 min', { value: '2', unit: ' min' }],
    ['€10.78', null],
    ['€5.24 – €5.79', null],
    ['5,24–5,79 €', null],
    ['1 h 20 min', null],
    ['Unknown', null],
    ['AC', null],
    ['5', null],
  ])('%s', (text, want) => {
    expect(figureParts(text)).toEqual(want)
  })

  // Intl's own spaces (no-break, narrow no-break) are kept: shown apart, the number and
  // its unit read as Intl wrote them.
  it.each(['fr-FR', 'sv-SE', 'en-GB'])('keeps what Intl writes in %s', (locale) => {
    for (const [v, unit] of [
      [12473, 'km'],
      [64, 'percent'],
      [18.1, 'kWhPer100km'],
      [-3, 'percent'],
    ] as const) {
      const text = formatQuantity(v, unit, locale) ?? ''
      const parts = figureParts(text)
      expect(parts, text).not.toBeNull()
      expect(`${parts?.value}${parts?.unit}`).toBe(text)
    }
  })
})
