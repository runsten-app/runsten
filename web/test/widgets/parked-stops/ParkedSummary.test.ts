import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { sessionKeys } from '@/entities/session'
import type { Stats } from '@/entities/stats'
import { ParkedSummary } from '@/widgets/parked-stops'
import session from '@fixtures/session.json'
import stats from '@fixtures/stats.json'
import { mountWith, testQueryClient, testRouter } from '@test/utils'

vi.mock('@/entities/session/extensions', () => ({ limitsPage: null }))
const mocks = vi.hoisted(() => ({ fetchStats: vi.fn() }))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats: mocks.fetchStats,
}))

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()
const withParked = (parked: Partial<Stats['totals']['parked']>) => {
  const s = structuredClone(stats) as Stats
  s.totals.parked = { ...s.totals.parked, ...parked }
  return s
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 26, 10))
  mocks.fetchStats.mockReset()
  mocks.fetchStats.mockResolvedValue(withParked({ soc_loss_pct_per_day: 0.21, power_w: 7 }))
})
afterEach(() => {
  vi.useRealTimers()
})

async function mounted(unavailable: string[] = []) {
  const router = testRouter()
  await router.push('/vehicles/v1/health')
  const queryClient = testQueryClient()
  if (unavailable.length)
    queryClient.setQueryData(sessionKeys.current(), {
      ...session,
      limits: { history_from: null, unavailable, unread_vehicles: [] },
    })
  const { wrapper } = mountWith(ParkedSummary, { router, queryClient, props: { vehicle: 'v1' } })
  await flushPromises()
  return wrapper
}

describe('ParkedSummary', () => {
  it('tells the loss while parked of the last 30 days, and leads to its stops over them', async () => {
    const w = await mounted()
    expect(mocks.fetchStats).toHaveBeenCalledWith(
      'v1',
      expect.objectContaining({
        from: new Date(2026, 7, 28).toISOString(),
        to: new Date(2026, 8, 27).toISOString(),
      }),
    )
    expect(w.find('h2').text()).toBe('Loss while parked')
    expect(w.find('dt').text()).toBe('Over the last 30 days')
    expect(plain(w.find('.value').text())).toBe('0.2% per day')
    expect(plain(w.find('.notes').text())).toContain('7 W lost on average')
    const link = w.find('a.open')
    expect(link.text()).toBe('See the detail, stop by stop')
    expect(link.attributes('href')).toBe('/vehicles/v1/stats/parked?from=2026-08-28&to=2026-09-26')
  })

  it('says when the loss is not known, and still tells the time asleep', async () => {
    mocks.fetchStats.mockResolvedValue(
      withParked({ soc_loss_pct_per_day: null, power_w: null, asleep_share: 0.95 }),
    )
    const w = await mounted()
    expect(w.find('.value').text()).toBe('Not known over the last 30 days')
    expect(plain(w.find('.notes').text())).toBe('asleep 95% of the time read')
    expect(w.find('a.open').exists()).toBe(true)
  })

  it('shows nothing to an account without the statistics', async () => {
    const w = await mounted(['stats'])
    expect(w.find('.parked-summary').exists()).toBe(false)
    expect(mocks.fetchStats).not.toHaveBeenCalled()
  })
})
