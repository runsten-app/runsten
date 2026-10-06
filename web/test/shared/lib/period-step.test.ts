import { describe, expect, it } from 'vitest'
import { stepPeriod, wholeMonths } from '@/shared/lib'

describe('wholeMonths', () => {
  it('counts the whole months of a period, none for another', () => {
    expect(wholeMonths({ from: '2026-10-01', to: '2026-10-31' })).toBe(1)
    expect(wholeMonths({ from: '2026-02-01', to: '2026-02-28' })).toBe(1)
    expect(wholeMonths({ from: '2025-11-01', to: '2026-10-31' })).toBe(12)
    expect(wholeMonths({ from: '2026-10-02', to: '2026-10-31' })).toBeUndefined()
    expect(wholeMonths({ from: '2026-10-01', to: '2026-10-30' })).toBeUndefined()
    expect(wholeMonths({ from: '2026-10-01' })).toBeUndefined()
  })
})

describe('stepPeriod', () => {
  it('steps a month by a month, to its own last day', () => {
    expect(stepPeriod({ from: '2026-03-01', to: '2026-03-31' }, -1)).toEqual({
      from: '2026-02-01',
      to: '2026-02-28',
    })
    expect(stepPeriod({ from: '2026-12-01', to: '2026-12-31' }, 1)).toEqual({
      from: '2027-01-01',
      to: '2027-01-31',
    })
  })

  it('steps a year, or twelve months, by twelve months', () => {
    expect(stepPeriod({ from: '2026-01-01', to: '2026-12-31' }, -1)).toEqual({
      from: '2025-01-01',
      to: '2025-12-31',
    })
    expect(stepPeriod({ from: '2025-11-01', to: '2026-10-31' }, 1)).toEqual({
      from: '2026-11-01',
      to: '2027-10-31',
    })
  })

  it('steps other periods by their number of days', () => {
    expect(stepPeriod({ from: '2026-10-03', to: '2026-10-09' }, 1)).toEqual({
      from: '2026-10-10',
      to: '2026-10-16',
    })
    expect(stepPeriod({ from: '2026-10-03', to: '2026-10-03' }, -1)).toEqual({
      from: '2026-10-02',
      to: '2026-10-02',
    })
  })

  it('has no step for a period open at an end', () => {
    expect(stepPeriod({ to: '2026-10-06' }, -1)).toBeUndefined()
    expect(stepPeriod({}, 1)).toBeUndefined()
  })
})
