import { describe, expect, it } from 'vitest'
import type { PeriodStats, Stats } from '@/entities/stats'
import {
  accountTotals,
  best,
  chargeSplit,
  costKnown,
  perHundred,
  shares,
  temperatureAxis,
} from '@/features/compare-vehicles'
import { instants, periodDays } from '@/features/compare-vehicles/model/garage'
import stats from '@fixtures/stats.json'

const totals = stats.totals as PeriodStats

// period gives a period's trips and charges, the rest of the fixture's.
function period(
  distance: number,
  cost: [number, number] | null,
  extra: { distanceUnknown?: number; charges?: number; costUnknown?: number } = {},
): PeriodStats {
  const charges = extra.charges ?? 1
  return {
    ...totals,
    trips: { ...totals.trips, distance_km: distance, distance_unknown: extra.distanceUnknown ?? 0 },
    charges: {
      ...totals.charges,
      count: charges,
      cost: { min_minor: cost?.[0] ?? 0, max_minor: cost?.[1] ?? 0 },
      cost_unknown: cost ? (extra.costUnknown ?? 0) : charges,
    },
  }
}

describe('periodDays', () => {
  const today = new Date(2026, 10, 11)
  it('is the last 30 days, today included', () => {
    expect(periodDays('30d', today)).toEqual({ from: '2026-10-13', to: '2026-11-11' })
  })
  it('is this month and the eleven before', () => {
    expect(periodDays('12m', today)).toEqual({ from: '2025-12-01', to: '2026-11-30' })
  })
  it('gives the API the start of the first day and of the day after the last', () => {
    // The tests' time zone is UTC.
    expect(instants({ from: '2026-10-13', to: '2026-11-11' })).toEqual({
      from: '2026-10-13T00:00:00.000Z',
      to: '2026-11-12T00:00:00.000Z',
    })
  })
})

describe('perHundred', () => {
  it('divides the cost by the distance, the lowest down and the highest up', () => {
    const p = period(300, [1000, 1001])
    expect(perHundred(p.trips, p.charges)).toEqual({ min_minor: 333, max_minor: 334 })
  })
  it('is unknown without a distance, or with a part unknown', () => {
    const none = period(0, [1000, 1000])
    expect(perHundred(none.trips, none.charges)).toBeNull()
    const lost = period(100, [1000, 1000], { distanceUnknown: 1 })
    expect(perHundred(lost.trips, lost.charges)).toBeNull()
    const unpriced = period(100, [1000, 1000], { charges: 2, costUnknown: 1 })
    expect(perHundred(unpriced.trips, unpriced.charges)).toBeNull()
  })
})

describe('costKnown', () => {
  it('is known without a charge, or with one charge of known cost', () => {
    expect(costKnown({ count: 0, cost_unknown: 0 })).toBe(true)
    expect(costKnown({ count: 2, cost_unknown: 1 })).toBe(true)
    expect(costKnown({ count: 2, cost_unknown: 2 })).toBe(false)
  })
})

describe('accountTotals', () => {
  it('adds the vehicles up, and what 100 km cost on average', () => {
    const t = accountTotals([period(100, [500, 600]), period(300, [1500, 1500])])
    expect(t.distanceKm).toBe(400)
    expect(t.cost).toEqual({ min_minor: 2000, max_minor: 2100 })
    expect(t.costKnown).toBe(true)
    expect(t.perHundred).toEqual({ min_minor: 500, max_minor: 525 })
  })
  it('has no average with a cost unknown, and no cost when none is known', () => {
    const partly = accountTotals([period(100, [500, 500]), period(100, null)])
    expect(partly.costKnown).toBe(true)
    expect(partly.costUnknown).toBe(1)
    expect(partly.perHundred).toBeNull()
    const never = accountTotals([period(100, null), period(100, null)])
    expect(never.costKnown).toBe(false)
  })
})

describe('chargeSplit', () => {
  it("adds the account's places, then AC and DC outside them, the rest unknown", () => {
    const s = chargeSplit(stats as Stats)
    // The fixture: a charge at a place, a reconstructed one without a position.
    expect(s).toEqual({ places: 26.4, ac: 0, dc: 0, unknown: 17.6 })
    const p = shares(s)
    expect(p?.places).toBeCloseTo(60)
    expect(p?.unknown).toBeCloseTo(40)
  })
  it('has no shares without energy', () => {
    expect(shares({ places: 0, ac: 0, dc: 0, unknown: 0 })).toBeNull()
  })
})

describe('temperatureAxis', () => {
  it("spans every vehicle's bands, empty ones between included", () => {
    expect(temperatureAxis([[{ min_c: 5 }, { min_c: 10 }], [{ min_c: -5 }]])).toEqual([
      -5, 0, 5, 10,
    ])
    expect(temperatureAxis([[], []])).toEqual([])
  })
})

describe('best', () => {
  it('is the lowest known value, with two to compare', () => {
    expect(best([3, null, 2.5])).toBe(2.5)
    expect(best([3, null])).toBeNull()
  })
})
