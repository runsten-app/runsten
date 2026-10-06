import type { Battery } from '@/entities/battery'

// The gauge's scale: a battery keeps most of its capacity for years, and a scale from 0
// would draw every vehicle full. Below GAUGE_MIN the arc stays empty, above 100 % full:
// the figure written under it is the exact one.
export const GAUGE_MIN = 70
export const GAUGE_MAX = 100

// Remaining is the estimated capacity as a share of the reference, in whole percent, with
// the share of its middle half (Q1 to Q3): the margin of the estimate.
export type Remaining = { pct: number; low: number; high: number }

// remainingOf is null until there is both a current capacity (5 charges) and a reference
// to compare it with.
export function remainingOf(b: Battery): Remaining | null {
  const { current, reference } = b
  if (!current || !reference) return null
  const share = (kwh: number) => Math.round((kwh / reference.capacity_kwh) * 100)
  return {
    pct: share(current.capacity_kwh),
    low: share(current.q1_kwh),
    high: share(current.q3_kwh),
  }
}

// arcShare is how much of the arc a percent fills, from 0 to 100, clamped to the scale.
export function arcShare(pct: number): number {
  const share = ((pct - GAUGE_MIN) / (GAUGE_MAX - GAUGE_MIN)) * 100
  return Math.min(100, Math.max(0, share))
}
