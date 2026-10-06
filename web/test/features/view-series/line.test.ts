import { describe, expect, it } from 'vitest'
import type { SeriesRun } from '@/entities/series'
import { readingsOf, seriesLine } from '@/features/view-series'
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
})
