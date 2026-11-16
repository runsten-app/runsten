import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Parked } from '@/entities/stats'
import { ApiError } from '@/shared/api'
import { ParkedStops } from '@/widgets/parked-stops'
import parked from '@fixtures/parked.json'
import { mountWith } from '@test/utils'

const { fetchParked } = vi.hoisted(() => ({ fetchParked: vi.fn() }))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchParked,
}))

beforeEach(() => {
  fetchParked.mockReset()
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function stops(p: Parked | Error, all = false) {
  if (p instanceof Error) fetchParked.mockRejectedValue(p)
  else fetchParked.mockResolvedValue(p)
  const { wrapper } = mountWith(ParkedStops, {
    props: {
      vehicle: 'v1',
      from: 'a',
      to: 'b',
      all,
    },
  })
  await flushPromises()
  return wrapper
}

describe('ParkedStops', () => {
  it('lists each stop of the period, newest first, with the events around it', async () => {
    const w = await stops(parked as Parked)
    expect(fetchParked).toHaveBeenCalledWith('v1', { from: 'a', to: 'b' })
    // min_stop_s 0: every stop counts in the rate.
    expect(w.find('h2').text()).toBe('4 stops of 0 mins or more')
    const rows = w.findAll('tbody tr')
    expect(rows).toHaveLength(4)
    // The oldest: a charge, then 2 h 55 parked, nothing lost, at 8.5 °C.
    const last = rows[3]
    expect(plain(last?.find('.duration').text())).toBe('2 hrs, 55 mins')
    expect(plain(last?.find('.loss').text())).toBe('0% 62% → 62%')
    expect(plain(last?.find('.power').text())).toBe('0 W')
    expect(plain(last?.find('.temperature').text())).toBe('8.5°C')
    const links = last?.findAll('th a') ?? []
    expect(links.map((a) => a.text())).toEqual(['after a charge', 'before a trip'])
    expect(links.map((a) => a.attributes('href'))).toEqual([
      '/vehicles/v1/charges/2026-09-28T04:00:00.000000Z',
      '/vehicles/v1/trips/2026-09-28T07:01:00.000000Z',
    ])
    // Unknown, never 0, where the state of charge is missing.
    expect(plain(rows[0]?.find('.loss').text())).toMatch(/^Unknown/)
    expect(plain(rows[0]?.find('.asleep').text())).toBe('0%')
    expect(plain(rows[1]?.find('.asleep').text())).toBe('100%')
    // Every stop is rated: no note on the short ones.
    expect(w.findAll('tr.short')).toHaveLength(0)
    expect(w.text()).not.toContain('not in the rate')
  })

  it('shows the stops of the rate only, as a vampire drain, and all of them when asked', async () => {
    const p = structuredClone(parked) as Parked
    p.min_stop_s = 3 * 3600
    for (const s of p.items) s.rated = s.time_s >= p.min_stop_s
    const w = await stops(p)
    expect(w.find('h2').text()).toBe('2 stops of 3 hrs or more')
    expect(w.findAll('tbody tr')).toHaveLength(2)
    expect(w.findAll('tr.short')).toHaveLength(0)

    const all = await stops(p, true)
    expect(all.find('h2').text()).toBe('4 stops')
    const short = all.findAll('tr.short')
    expect(short).toHaveLength(2)
    expect(short[0]?.find('.duration').text()).toContain('not in the rate')
    expect(plain(all.find('.note').text())).toMatch(
      /^A stop under 3 hrs counts in the period's loss/,
    )

    // None long enough: said, with the way to the others.
    for (const s of p.items) s.rated = false
    const none = await stops(p)
    expect(none.find('.none').text()).toBe('No stop of 3 hrs or more in this period.')
  })

  it('says when there is no stop, or when they could not be read', async () => {
    const none = await stops({ ...(parked as Parked), items: [] })
    expect(none.find('.none').text()).toBe('No stop in this period.')
    const failed = await stops(new ApiError(500, 'internal', ''))
    expect(failed.find('.v-alert').text()).toBe('The stops could not be loaded.')
    const gone = await stops(new ApiError(404, 'not_found', ''))
    expect(gone.find('.v-alert').text()).not.toBe('The stops could not be loaded.')
  })
})
