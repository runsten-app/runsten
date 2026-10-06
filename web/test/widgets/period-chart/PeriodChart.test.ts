import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Stats } from '@/entities/stats'
import { sessionKeys } from '@/entities/session'
import { ChartFrame, type Series } from '@/shared/ui/chart'
import { PeriodChart } from '@/widgets/period-chart'
import session from '@fixtures/session.json'
import stats from '@fixtures/stats.json'
import totals from '@fixtures/stats-totals.json'
import { mountWith, testQueryClient, testRouter } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const { fetchStats } = vi.hoisted(() => ({ fetchStats: vi.fn() }))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats,
}))

beforeEach(() => {
  fetchStats.mockReset()
  fetchStats.mockResolvedValue(stats)
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()
const september = {
  from: '2026-09-01T00:00:00.000Z',
  to: '2026-10-01T00:00:00.000Z',
  days: { from: '2026-09-01', to: '2026-09-30' },
}

async function mounted(kind: 'trips' | 'charges', period = september, unavailable: string[] = []) {
  const router = testRouter()
  await router.push(`/vehicles/v1/${kind}?from=2026-09-01&to=2026-09-30`)
  const queryClient = testQueryClient()
  if (unavailable.length)
    queryClient.setQueryData(sessionKeys.current(), {
      ...session,
      limits: { history_from: null, unavailable, unread_vehicles: [] },
    })
  const { wrapper } = mountWith(PeriodChart, {
    router,
    queryClient,
    props: { vehicle: 'v1', kind, ...period },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('PeriodChart', () => {
  it("draws the distance of the list's period, split by its length", async () => {
    const { wrapper } = await mounted('trips')
    expect(fetchStats).toHaveBeenCalledWith('v1', {
      from: september.from,
      to: september.to,
      tz: 'UTC',
      bucket: 'day',
    })
    const frame = wrapper.findComponent(ChartFrame)
    expect(frame.props('title')).toBe('Distance')
    expect((frame.props('series') as Series[])[0]?.data).toEqual([73, 0, 0])
    expect(plain(wrapper.find('canvas').attributes('aria-label'))).toBe(
      'Distance: 73 km in all, the most for 27 Sept (73 km).',
    )
    expect(frame.props('hint')).toBe('Select a bar to narrow the list to its interval.')
  })

  it('draws the energy of the charges, AC and DC stacked', async () => {
    const { wrapper } = await mounted('charges')
    const frame = wrapper.findComponent(ChartFrame)
    expect(frame.props('stacked')).toBe(true)
    expect((frame.props('series') as Series[]).map((s) => s.label)).toEqual([
      'AC',
      'DC',
      'Unknown type',
    ])
  })

  // The second interval: 28 September in Paris, 22:00 to 22:00 UTC; its days here, in UTC.
  it('narrows the list to the interval pressed', async () => {
    const { wrapper, router } = await mounted('trips')
    wrapper.findComponent(ChartFrame).vm.$emit('select', 1)
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toEqual({ from: '2026-09-28', to: '2026-09-29' }),
    )
    expect(router.currentRoute.value.name).toBe('trips')
  })

  it('splits by month a period open at the start', async () => {
    await mounted('trips', { from: undefined, to: undefined, days: {} } as never)
    expect(fetchStats).toHaveBeenCalledWith('v1', expect.objectContaining({ bucket: 'month' }))
  })

  it('draws nothing without an event, a single interval, or the statistics by interval', async () => {
    const none = structuredClone(stats) as Stats
    none.totals.trips.count = 0
    fetchStats.mockResolvedValue(none)
    expect((await mounted('trips')).wrapper.find('.period-chart').exists()).toBe(false)
    const one = structuredClone(stats) as Stats
    one.buckets = one.buckets.slice(0, 1)
    fetchStats.mockResolvedValue(one)
    expect((await mounted('trips')).wrapper.find('.period-chart').exists()).toBe(false)
    // Without the statistics by interval, the totals alone, as the API gives them.
    fetchStats.mockReset()
    fetchStats.mockResolvedValue(totals)
    const { wrapper } = await mounted('trips', september, ['stats'])
    expect(fetchStats).toHaveBeenCalledWith('v1', expect.objectContaining({ bucket: undefined }))
    expect(wrapper.find('.period-chart').exists()).toBe(false)
  })
})
