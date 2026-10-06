import { describe, expect, it } from 'vitest'
import type { Battery } from '@/entities/battery'
import battery from '@fixtures/battery.json'
import empty from '@fixtures/battery-empty.json'
import { arcShare, remainingOf } from '@/widgets/battery-tiles/model/remaining'

describe('remainingOf', () => {
  it('is the estimate and its middle half over the reference, in whole percent', () => {
    // 73.5, 73 and 73.75 kWh over 75.
    expect(remainingOf(battery as Battery)).toEqual({ pct: 98, low: 97, high: 98 })
  })

  it('is null without a current capacity or a reference', () => {
    expect(remainingOf(empty as Battery)).toBeNull()
    expect(remainingOf({ ...(battery as Battery), reference: null })).toBeNull()
  })
})

describe('arcShare', () => {
  it.each([
    [100, 100],
    [85, 50],
    [70, 0],
    [60, 0],
    [104, 100],
  ])('fills %s %% as %s of the arc', (pct, share) => {
    expect(arcShare(pct)).toBe(share)
  })
})
