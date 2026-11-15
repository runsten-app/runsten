import { describe, expect, it } from 'vitest'
import type { Battery } from '@/entities/battery'
import battery from '@fixtures/battery.json'
import empty from '@fixtures/battery-empty.json'
import { capacityTrend, runs } from '@/widgets/battery-tiles/model/trend'

const b = battery as Battery

describe('capacityTrend', () => {
  it('is the monthly medians and their middle half, the reference within its bounds', () => {
    const tr = capacityTrend(b)
    expect(tr?.median).toHaveLength(b.months.length)
    expect(tr?.median[0]).toBe(76)
    expect(tr?.q1[0]).toBe(76)
    expect(tr?.q3[0]).toBe(76.25)
    expect(tr?.reference).toBe(75)
    expect(tr?.first).toEqual({ start: '2025-08-01T00:00:00Z', kwh: 76 })
    const all = [...(tr?.q1 ?? []), ...(tr?.q3 ?? []), 75].filter((v) => v !== null)
    expect(tr?.min).toBeLessThan(Math.min(...all))
    expect(tr?.max).toBeGreaterThan(Math.max(...all))
  })

  it('keeps a month without a median as a hole, never a zero', () => {
    const months = b.months.map((m, i) => (i === 1 ? { ...m, capacity: null } : m))
    const tr = capacityTrend({ ...b, months })
    expect(tr?.median[1]).toBeNull()
    expect(tr?.q1[1]).toBeNull()
  })

  it('is null under two months with a median', () => {
    expect(capacityTrend(empty as Battery)).toBeNull()
    const one = b.months.map((m, i) => (i === 0 ? m : { ...m, capacity: null }))
    expect(capacityTrend({ ...b, months: one })).toBeNull()
  })

  it('draws without a reference', () => {
    expect(capacityTrend({ ...b, reference: null })?.reference).toBeNull()
  })
})

describe('runs', () => {
  it('splits a series at its holes', () => {
    expect(runs([1, 2, null, 3, null, null])).toEqual([
      [
        { i: 0, v: 1 },
        { i: 1, v: 2 },
      ],
      [{ i: 3, v: 3 }],
    ])
    expect(runs([null])).toEqual([])
  })
})
