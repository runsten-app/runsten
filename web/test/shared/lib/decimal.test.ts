import { describe, expect, it } from 'vitest'
import { decimalPlaces, parseDecimal } from '@/shared/lib'

describe('parseDecimal', () => {
  it.each([
    ['0.2142', 0.2142],
    ['0,2142', 0.2142],
    [' 45,764 ', 45.764],
    ['-4.8357', -4.8357],
    ['+1', 1],
    ['100', 100],
    ['.5', 0.5],
    [',5', 0.5],
    ['7.', 7],
    ['0', 0],
  ])('reads %j', (s, want) => expect(parseDecimal(s)).toBe(want))

  it.each(['', '  ', 'abc', '1.2.3', '1,2,3', '1 000', '1e3', '0x10', '-', '.', 'Infinity'])(
    'refuses %j',
    (s) => expect(parseDecimal(s)).toBeNull(),
  )
})

describe('decimalPlaces', () => {
  it.each([
    ['0.2142', 4],
    ['0,21420', 4],
    ['0.123456', 6],
    ['12', 0],
    ['12.000', 0],
    ['0.10000', 1],
  ])('%j has %i', (s, want) => expect(decimalPlaces(s)).toBe(want))
})
