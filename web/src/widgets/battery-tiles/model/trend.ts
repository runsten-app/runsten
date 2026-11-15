import type { Battery } from '@/entities/battery'

// CapacityTrend is what the health page's battery tile draws of the estimated capacity:
// the monthly medians of the battery's page, in kWh, with their middle half (Q1 to Q3)
// and the reference they are compared with. A month without a median is a hole, never
// a zero: the line breaks there.
export type CapacityTrend = {
  // The months' starts, from the first with a median to the last: the ends sit at the
  // chart's edges, where their names are.
  starts: string[]
  median: (number | null)[]
  q1: (number | null)[]
  q3: (number | null)[]
  reference: number | null
  // The first and last months with a median, by their start: the ends the tile names.
  first: { start: string; kwh: number }
  last: { start: string; kwh: number }
  // The vertical bounds, the reference and the band included, with a margin.
  min: number
  max: number
}

// capacityTrend is null under two months with a median: one point tells no evolution,
// and the tile then keeps its figure alone.
export function capacityTrend(b: Battery): CapacityTrend | null {
  const drawn = b.months.filter((m) => m.capacity)
  const first = drawn[0]
  const last = drawn.at(-1)
  if (!first?.capacity || !last?.capacity || drawn.length < 2) return null
  // The span from the first to the last month with a median: a month without one at
  // either end (a trip after the last estimate) would move the line off the edge its
  // month is named at. Holes within stay.
  const span = b.months.slice(b.months.indexOf(first), b.months.indexOf(last) + 1)
  const median = span.map((m) => m.capacity?.capacity_kwh ?? null)
  const q1 = span.map((m) => m.capacity?.q1_kwh ?? null)
  const q3 = span.map((m) => m.capacity?.q3_kwh ?? null)
  const reference = b.reference?.capacity_kwh ?? null
  const values = [...q1, ...q3, ...median, reference].filter((v): v is number => v !== null)
  const lo = Math.min(...values)
  const hi = Math.max(...values)
  // As the battery page's axis, never from zero: a fall of a few kWh would read flat.
  const margin = (hi - lo || hi) * 0.08
  return {
    starts: span.map((m) => m.start),
    median,
    q1,
    q3,
    reference,
    first: { start: first.start, kwh: first.capacity.capacity_kwh },
    last: { start: last.start, kwh: last.capacity.capacity_kwh },
    min: lo - margin,
    max: hi + margin,
  }
}

// runs splits a series at its holes, each run the indexes and values it joins.
export function runs(values: (number | null)[]): { i: number; v: number }[][] {
  const out: { i: number; v: number }[][] = []
  let run: { i: number; v: number }[] = []
  values.forEach((v, i) => {
    if (v === null) {
      if (run.length) out.push(run)
      run = []
    } else run.push({ i, v })
  })
  if (run.length) out.push(run)
  return out
}
