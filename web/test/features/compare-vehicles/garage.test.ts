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

// period gives a period's trips, their costs, and its charges, the rest of the
// fixture's. tripCost is the trips' cost, null when none is known; per100 their cost per
// 100 km, the API's.
function period(
  distance: number,
  cost: [number, number] | null,
  extra: {
    distanceUnknown?: number
    charges?: number
    costUnknown?: number
    tripCost?: [number, number] | null
    tripCostUnknown?: number
    per100?: [number, number] | null
  } = {},
): PeriodStats {
  const charges = extra.charges ?? 1
  const tripCost = extra.tripCost === undefined ? cost : extra.tripCost
  const per100 =
    extra.per100 !== undefined
      ? extra.per100
      : tripCost && distance > 0
        ? ([
            Math.floor((tripCost[0] * 100) / distance),
            Math.ceil((tripCost[1] * 100) / distance),
          ] as [number, number])
        : null
  return {
    ...totals,
    trips: {
      ...totals.trips,
      count: 2,
      distance_km: distance,
      distance_unknown: extra.distanceUnknown ?? 0,
      cost: { min_minor: tripCost?.[0] ?? 0, max_minor: tripCost?.[1] ?? 0 },
      cost_unknown: tripCost ? (extra.tripCostUnknown ?? 0) : 2,
      cost_per_100km: per100 && { min_minor: per100[0], max_minor: per100[1] },
    },
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
  it("is the statistics' cost per 100 km of the trips, whatever the charges cost", () => {
    const p = period(300, [9000, 9000], { tripCost: [1000, 1001] })
    expect(perHundred(p.trips)).toEqual({ min_minor: 333, max_minor: 334 })
  })
  it('is unknown where the statistics have none: a plug-in hybrid, no trip of known cost', () => {
    expect(perHundred(period(300, [1000, 1000], { per100: null }).trips)).toBeNull()
    expect(perHundred(period(300, [1000, 1000], { tripCost: null }).trips)).toBeNull()
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
  it("adds the vehicles up, and what 100 km cost on average, from the trips' costs", () => {
    const t = accountTotals([
      period(100, [9000, 9000], { tripCost: [500, 600] }),
      period(300, [1500, 1500]),
    ])
    // A range over the account: its bounds. The charges' cost is what was paid, the
    // average per 100 km what the trips used.
    expect(t.distanceKm).toBe(400)
    expect(t.cost).toEqual({ min_minor: 10500, max_minor: 10500 })
    expect(t.costKnown).toBe(true)
    expect(t.perHundred).toEqual({ min_minor: 500, max_minor: 525 })
    const exact = accountTotals([period(100, [500, 500]), period(200, [1001, 1001])])
    expect(exact.perHundred).toEqual({ min_minor: 500, max_minor: 500 })
  })
  it('has no average with a trip of unknown cost or distance, or a plug-in hybrid', () => {
    const partly = accountTotals([
      period(100, [500, 500]),
      period(100, [500, 500], { tripCostUnknown: 1 }),
    ])
    expect(partly.perHundred).toBeNull()
    const lost = accountTotals([period(100, [500, 500], { distanceUnknown: 1 })])
    expect(lost.perHundred).toBeNull()
    const hybrid = accountTotals([
      period(100, [500, 500]),
      period(100, [500, 500], { per100: null }),
    ])
    expect(hybrid.perHundred).toBeNull()
  })
  it('has no cost when none is known', () => {
    const partly = accountTotals([period(100, [500, 500]), period(100, null)])
    expect(partly.costKnown).toBe(true)
    expect(partly.costUnknown).toBe(1)
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
