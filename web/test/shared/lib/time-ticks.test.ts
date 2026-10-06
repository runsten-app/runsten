import { describe, expect, it } from 'vitest'
import { timeTicks } from '@/shared/lib'

const at = (iso: string) => Date.parse(iso)
const iso = (ticks: number[]) => ticks.map((t) => new Date(t).toISOString())

describe('timeTicks', () => {
  it('graduates a charge on round times of the clock', () => {
    const { ticks, daily } = timeTicks(at('2026-09-28T18:20:00Z'), at('2026-09-28T21:56:00Z'), 6)
    expect(daily).toBe(false)
    expect(iso(ticks)).toEqual([
      '2026-09-28T19:00:00.000Z',
      '2026-09-28T20:00:00.000Z',
      '2026-09-28T21:00:00.000Z',
    ])
  })

  it('takes a finer step for a short window', () => {
    const { ticks } = timeTicks(at('2026-09-28T18:07:00Z'), at('2026-09-28T18:40:00Z'), 6)
    expect(iso(ticks)).toEqual([
      '2026-09-28T18:10:00.000Z',
      '2026-09-28T18:20:00.000Z',
      '2026-09-28T18:30:00.000Z',
      '2026-09-28T18:40:00.000Z',
    ])
  })

  it('graduates a week on its midnights', () => {
    const { ticks, daily } = timeTicks(at('2026-09-21T19:05:00Z'), at('2026-09-28T19:05:00Z'), 7)
    expect(daily).toBe(true)
    expect(iso(ticks)).toEqual([
      '2026-09-22T00:00:00.000Z',
      '2026-09-23T00:00:00.000Z',
      '2026-09-24T00:00:00.000Z',
      '2026-09-25T00:00:00.000Z',
      '2026-09-26T00:00:00.000Z',
      '2026-09-27T00:00:00.000Z',
      '2026-09-28T00:00:00.000Z',
    ])
  })
})
