import { describe, expect, it } from 'vitest'
import { groupByDay } from '@/shared/lib'

const at = (after: string) => ({ start: { after } })

describe('groupByDay', () => {
  it('splits by the day each event started on, in order', () => {
    const a = at('2026-09-28T16:35:00Z')
    const b = at('2026-09-28T06:55:00Z')
    const c = at('2026-09-27T22:30:00Z')
    expect(groupByDay([a, b, c], 'UTC')).toEqual([
      { day: '2026-09-28', at: a.start.after, items: [a, b] },
      { day: '2026-09-27', at: c.start.after, items: [c] },
    ])
    // 22:30 UTC on the 27th is the 28th in Stockholm.
    expect(groupByDay([a, b, c], 'Europe/Stockholm').map((g) => g.day)).toEqual(['2026-09-28'])
  })

  it('has no group without events', () => expect(groupByDay([])).toEqual([]))
})
