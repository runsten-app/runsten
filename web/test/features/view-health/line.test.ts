import { describe, expect, it } from 'vitest'
import type { TyreRun } from '@/entities/health'
import { tyreLine } from '@/features/view-health'
import tyres from '@fixtures/tyres.json'

const runs = tyres.runs as TyreRun[]
const at = (iso: string) => Date.parse(iso)

describe('tyreLine', () => {
  it('breaks the line between two runs, and where a reading lacks the value', () => {
    expect(tyreLine(runs, (r) => r.rear_left_kpa)).toEqual([
      { x: at('2026-09-28T14:00:00Z'), y: 270 },
      { x: at('2026-09-28T15:00:00Z'), y: 270 },
      { x: at('2026-09-28T18:00:00Z') - 1, y: null },
      { x: at('2026-09-28T18:00:00Z'), y: 248 },
      { x: at('2026-09-28T19:00:00Z'), y: 248 },
    ])
    expect(tyreLine(runs, (r) => r.rear_right_kpa).every((p) => p.y === null)).toBe(true)
    expect(tyreLine([], (r) => r.rear_left_kpa)).toEqual([])
  })
})
