import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { sessionKeys } from '@/entities/session'
import type { Stats } from '@/entities/stats'
import { ParkedPage } from '@/pages/parked'
import parked from '@fixtures/parked.json'
import session from '@fixtures/session.json'
import stats from '@fixtures/stats.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testQueryClient, testRouter } from '@test/utils'

vi.mock('@/entities/session/extensions', () => ({ limitsPage: null }))
const mocks = vi.hoisted(() => ({
  fetchStats: vi.fn(),
  fetchParked: vi.fn(),
  fetchVehicles: vi.fn(),
}))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats: mocks.fetchStats,
  fetchParked: mocks.fetchParked,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
}))

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 26, 10))
  Object.values(mocks).forEach((m) => m.mockReset())
  const s = structuredClone(stats) as Stats
  s.totals.parked = { ...s.totals.parked, soc_loss_pct_per_day: 0.21, power_w: 7 }
  mocks.fetchStats.mockResolvedValue(s)
  mocks.fetchParked.mockResolvedValue(parked)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
})
afterEach(() => {
  vi.useRealTimers()
})

const vehicle = vehicles.items[0]?.id ?? ''
const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function page(query = '', unavailable: string[] = []) {
  const router = testRouter()
  await router.push(`/vehicles/${vehicle}/stats/parked${query}`)
  const queryClient = testQueryClient()
  if (unavailable.length)
    queryClient.setQueryData(sessionKeys.current(), {
      ...session,
      limits: { history_from: null, unavailable, unread_vehicles: [] },
    })
  const { wrapper } = mountWith(ParkedPage, { router, queryClient, props: { vehicle } })
  await flushPromises()
  return { wrapper, router }
}

describe('ParkedPage', () => {
  it('shows the rate, then each stop of the period, in the statistics’ tab, under the tile’s name', async () => {
    const { wrapper } = await page('?from=2026-09-01&to=2026-09-30')
    expect(mocks.fetchParked).toHaveBeenCalledWith(vehicle, {
      from: '2026-09-01T00:00:00.000Z',
      to: '2026-10-01T00:00:00.000Z',
    })
    expect(wrapper.find('.v-main h1').text()).toBe('Loss while parked')
    expect(plain(wrapper.find('.rate').text())).toBe('0.2% per day · 7 W lost on average')
    expect(wrapper.findAll('.parked-stops tbody tr')).toHaveLength(4)
    expect(wrapper.find('.v-tab[aria-current="page"]').text()).toBe('Stats')
    // Every stop counts in the rate: nothing to add.
    expect(wrapper.find('.toggle-short').exists()).toBe(false)
  })

  it('keeps the short stops shown in the URL', async () => {
    const p = structuredClone(parked)
    p.min_stop_s = 3 * 3600
    for (const s of p.items) s.rated = s.time_s >= p.min_stop_s
    mocks.fetchParked.mockResolvedValue(p)
    const { wrapper, router } = await page('?from=2026-09-01&to=2026-09-30')
    expect(wrapper.findAll('.parked-stops tbody tr')).toHaveLength(2)
    // Beside the period, with what it adds.
    const toggle = wrapper.find('.period-picker + .toggle-short')
    expect(toggle.find('label').text()).toBe('Include the 2 stops under 3 hrs')
    const input = () => wrapper.find<HTMLInputElement>('.toggle-short input')
    expect(input().element.checked).toBe(false)
    await input().setValue(true)
    await vi.waitFor(() => expect(router.currentRoute.value.query.short).toBe('1'))
    await flushPromises()
    expect(wrapper.findAll('.parked-stops tbody tr')).toHaveLength(4)
    expect(input().element.checked).toBe(true)
    // Its label stays: the switch tells its state.
    expect(wrapper.find('.toggle-short label').text()).toBe('Include the 2 stops under 3 hrs')
    await input().setValue(false)
    await vi.waitFor(() => expect(router.currentRoute.value.query.short).toBeUndefined())
  })

  it('takes this month without a period', async () => {
    const { router } = await page()
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toEqual({ from: '2026-09-01', to: '2026-09-30' }),
    )
  })

  it('says so where the offer leaves the statistics out', async () => {
    const { wrapper } = await page('', ['stats'])
    expect(mocks.fetchParked).not.toHaveBeenCalled()
    expect(wrapper.find('.parked-stops').exists()).toBe(false)
  })
})
