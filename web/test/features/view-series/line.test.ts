import { describe, expect, it } from 'vitest'
import type { SeriesRun } from '@/entities/series'
import { readingsOf, seriesLine, splitRuns } from '@/features/view-series'
import series from '@fixtures/series.json'

const runs = series.runs as SeriesRun[]
const at = (iso: string) => Date.parse(iso)

describe('seriesLine', () => {
  it('breaks the line between two runs, and where a reading lacks the value', () => {
    expect(seriesLine(runs, (r) => r.soc_pct)).toEqual([
      { x: at('2026-09-28T15:00:00Z'), y: 50 },
      { x: at('2026-09-28T17:26:00Z') - 1, y: null },
      { x: at('2026-09-28T17:26:00Z'), y: 47 },
      { x: at('2026-09-28T18:20:00Z'), y: 47 },
      { x: at('2026-09-28T18:30:00Z'), y: 48 },
      { x: at('2026-09-28T18:40:00Z'), y: 50 },
    ])
    expect(seriesLine(runs, (r) => r.range_km).at(-1)).toEqual({
      x: at('2026-09-28T18:40:00Z'),
      y: null,
    })
    expect(seriesLine([], (r) => r.soc_pct)).toEqual([])
  })

  it('lists every reading, oldest first', () => {
    expect(readingsOf(runs).map((r) => r.at)).toEqual([
      '2026-09-28T15:00:00Z',
      '2026-09-28T17:26:00Z',
      '2026-09-28T18:20:00Z',
      '2026-09-28T18:30:00Z',
      '2026-09-28T18:40:00Z',
    ])
  })

  // A charge's first reading charging, after ten minutes unread at a parked vehicle's
  // pace: no line from the reading before it.
  it('breaks a run at an instant', () => {
    const split = splitRuns(runs, '2026-09-28T18:30:00Z')
    expect(split.map((r) => r.readings.map((x) => x.at))).toEqual([
      ['2026-09-28T15:00:00Z'],
      ['2026-09-28T17:26:00Z', '2026-09-28T18:20:00Z'],
      ['2026-09-28T18:30:00Z', '2026-09-28T18:40:00Z'],
    ])
    expect(seriesLine(split, (r) => r.power_w).map((p) => p.y)).toEqual([
      0,
      null,
      0,
      0,
      null,
      7400,
      7350,
    ])
    // An instant at the start of a run, or past it, leaves the runs as they are.
    expect(splitRuns(runs, '2026-09-28T17:26:00Z')).toEqual(runs)
    expect(splitRuns(runs, '2026-09-28T19:00:00Z')).toEqual(runs)
  })
})
