import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Band, Series } from '@/shared/ui/chart'
import { SocTimeline } from '@/widgets/soc-timeline'
import charges from '@fixtures/charges.json'
import series from '@fixtures/series.json'
import trips1 from '@fixtures/trips-page-1.json'
import trips2 from '@fixtures/trips-page-2.json'
import { mountWith } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const mocks = vi.hoisted(() => ({
  fetchSeries: vi.fn(),
  fetchTrips: vi.fn(),
  fetchCharges: vi.fn(),
}))
vi.mock('@/entities/series', async (original) => ({
  ...(await original<typeof import('@/entities/series')>()),
  fetchSeries: mocks.fetchSeries,
}))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  fetchTrips: mocks.fetchTrips,
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchCharges: mocks.fetchCharges,
}))

const now = new Date('2026-09-28T19:02:00Z')
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(now)
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchSeries.mockResolvedValue(series)
  mocks.fetchTrips.mockImplementation((_v: string, q: { cursor?: string }) =>
    Promise.resolve(q.cursor ? trips2 : trips1),
  )
  mocks.fetchCharges.mockResolvedValue(charges)
})
afterEach(() => {
  vi.useRealTimers()
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mounted() {
  const { wrapper } = mountWith(SocTimeline, { props: { vehicle: 'v1' } })
  await flushPromises()
  return wrapper
}

describe('SocTimeline', () => {
  it('asks the last 7 days, on a window that moves every five minutes', async () => {
    await mounted()
    expect(mocks.fetchSeries).toHaveBeenCalledWith('v1', {
      from: '2026-09-21T19:00:00.000Z',
      to: '2026-09-28T19:00:00.000Z',
    })
    expect(mocks.fetchTrips).toHaveBeenCalledWith('v1', {
      from: '2026-09-21T19:00:00.000Z',
      to: undefined,
      cursor: undefined,
    })
  })

  it('draws the state of charge over bands of every trip and charge', async () => {
    const w = await mounted()
    const frame = w.findComponent({ name: 'ChartFrame' })
    const [soc] = frame.props('series') as Series[]
    expect(soc?.data).toHaveLength(6)
    const bands = frame.props('bands') as Band[]
    // Every page of the trips: the second is loaded too.
    expect(mocks.fetchTrips).toHaveBeenCalledTimes(2)
    expect(bands.map((b) => b.label)).toEqual([
      ...Array(trips1.items.length + trips2.items.length).fill('Driving'),
      'Charging',
      'Charging',
    ])
    // The evening charge: surely from 18:30 to 21:55, maybe from 18:20 to 21:56; the
    // reconstructed one is sure of nothing.
    const charging = bands.filter((b) => b.label === 'Charging')
    expect(charging[0]).toMatchObject({
      from: Date.parse('2026-09-28T18:20:00Z'),
      to: Date.parse('2026-09-28T21:56:00Z'),
      sure: { from: Date.parse('2026-09-28T18:30:00Z'), to: Date.parse('2026-09-28T21:55:00Z') },
    })
    expect(charging[1]?.sure).toBeUndefined()
    expect(plain(w.find('canvas').attributes('aria-label'))).toBe(
      'Over the last 7 days, from 50% to 50%, between 47% and 50%; 3 trips and 2 charges.',
    )
  })

  it("tells each day's lowest and highest state of charge, and its events", async () => {
    const w = await mounted()
    const rows = w.findAll('tbody tr').map((r) => r.findAll('th, td').map((c) => plain(c.text())))
    expect(rows).toHaveLength(8)
    expect(rows.at(-1)).toEqual(['28 Sept 2026', '47%', '50%', '3', '2'])
    expect(rows[0]).toEqual(['21 Sept 2026', 'Unknown', 'Unknown', '0', '0'])
  })

  it('says so when nothing was read', async () => {
    mocks.fetchSeries.mockResolvedValue({ ...series, runs: [] })
    const w = await mounted()
    expect(w.find('.empty').text()).toBe('Nothing was read in the last 7 days.')
    expect(w.find('canvas').exists()).toBe(false)
  })
})
