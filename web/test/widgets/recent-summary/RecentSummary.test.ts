import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { sessionKeys } from '@/entities/session'
import { ChartFrame, type Series } from '@/shared/ui/chart'
import { RecentSummary } from '@/widgets/recent-summary'
import session from '@fixtures/session.json'
import stats from '@fixtures/stats.json'
import empty from '@fixtures/stats-empty.json'
import totals from '@fixtures/stats-totals.json'
import { mountWith, testQueryClient, testRouter } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const { fetchStats } = vi.hoisted(() => ({ fetchStats: vi.fn() }))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats,
}))

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 29, 10))
  fetchStats.mockReset()
  fetchStats.mockResolvedValue(stats)
})
afterEach(() => {
  vi.useRealTimers()
})

async function mounted(unavailable: string[] = []) {
  const router = testRouter()
  await router.push('/vehicles/v1')
  const queryClient = testQueryClient()
  if (unavailable.length)
    queryClient.setQueryData(sessionKeys.current(), {
      ...session,
      limits: { history_from: null, unavailable, unread_vehicles: [] },
    })
  const { wrapper } = mountWith(RecentSummary, { router, queryClient, props: { vehicle: 'v1' } })
  await flushPromises()
  return { wrapper, router }
}

describe('RecentSummary', () => {
  it('sums the last 30 days, today included, and draws them by day', async () => {
    const { wrapper } = await mounted()
    expect(fetchStats).toHaveBeenCalledWith('v1', {
      from: '2026-08-31T00:00:00.000Z',
      to: '2026-09-30T00:00:00.000Z',
      tz: 'UTC',
      bucket: 'day',
    })
    expect(wrapper.find('h2').text()).toBe('Last 30 days')
    expect(wrapper.find('.tile.distance dd').text()).toBe('73 km')
    expect(wrapper.find('.tile.consumption dt').text()).toBe('Consumption')
    expect(wrapper.find('.tile.energy dd').text()).toContain('44')
    const frame = wrapper.findComponent(ChartFrame)
    expect((frame.props('series') as Series[])[0]?.data).toEqual([73, 0, 0])
  })

  it('opens the same days in the statistics, and a day in the trips', async () => {
    const { wrapper, router } = await mounted()
    expect(wrapper.find('a.open-stats').attributes('href')).toBe(
      '/vehicles/v1/stats?from=2026-08-31&to=2026-09-29',
    )
    wrapper.findComponent(ChartFrame).vm.$emit('select', 0)
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('trips')
    expect(router.currentRoute.value.query).toEqual({ from: '2026-09-27', to: '2026-09-27' })
  })

  it('keeps the totals only where the offer leaves out the statistics by interval', async () => {
    fetchStats.mockResolvedValue(totals)
    const { wrapper } = await mounted(['stats'])
    expect(fetchStats).toHaveBeenCalledWith('v1', expect.not.objectContaining({ bucket: 'day' }))
    expect(wrapper.find('.recent-summary').exists()).toBe(true)
    expect(wrapper.findComponent(ChartFrame).exists()).toBe(false)
  })

  it('shows nothing without a trip or a charge', async () => {
    fetchStats.mockResolvedValue(empty)
    const { wrapper } = await mounted()
    expect(wrapper.find('.recent-summary').exists()).toBe(false)
  })
})
