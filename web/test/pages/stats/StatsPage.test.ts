import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { h } from 'vue'
import { StatsPage } from '@/pages/stats'
import stats from '@fixtures/stats.json'
import vehicles from '@fixtures/vehicles.json'
import { sessionKeys } from '@/entities/session'
import { i18n } from '@/shared/i18n'
import session from '@fixtures/session.json'
import settings from '@fixtures/settings.json'
import unset from '@fixtures/settings-unset.json'
import { mountWith, testQueryClient, testRouter } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
// The public build's: no page lifts the limits.
vi.mock('@/entities/session/extensions', () => ({ limitsPage: null }))
// What the costs' view shows where the offer lacks them: none in the public build, one in a test.
const extension = vi.hoisted(() => ({ costsUnavailable: null as unknown }))
vi.mock('@/pages/stats/extensions', () => ({
  get costsUnavailable() {
    return extension.costsUnavailable
  },
}))
const mocks = vi.hoisted(() => ({
  fetchStats: vi.fn(),
  fetchVehicles: vi.fn(),
  fetchSettings: vi.fn(),
}))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats: mocks.fetchStats,
}))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  fetchSettings: mocks.fetchSettings,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
}))

beforeEach(() => {
  extension.costsUnavailable = null
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 26, 10))
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchStats.mockResolvedValue(stats)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
  mocks.fetchSettings.mockResolvedValue(settings)
})
afterEach(() => {
  vi.useRealTimers()
})

const vehicle = vehicles.items[0]?.id ?? ''

async function page(query = '', unavailable: string[] = []) {
  const router = testRouter()
  await router.push(`/vehicles/${vehicle}/stats${query}`)
  const queryClient = testQueryClient()
  if (unavailable.length)
    queryClient.setQueryData(sessionKeys.current(), {
      ...session,
      limits: { history_from: null, unavailable, unread_vehicles: [] },
    })
  const { wrapper } = mountWith(StatsPage, { router, queryClient, props: { vehicle } })
  await flushPromises()
  return { wrapper, router }
}

describe('StatsPage', () => {
  it('shows this month by day without a period, and never the whole history first', async () => {
    const { wrapper, router } = await page()
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toEqual({ from: '2026-09-01', to: '2026-09-30' }),
    )
    await flushPromises()
    expect(mocks.fetchStats).toHaveBeenCalledOnce()
    expect(mocks.fetchStats).toHaveBeenCalledWith(vehicle, {
      from: '2026-09-01T00:00:00.000Z',
      to: '2026-10-01T00:00:00.000Z',
      tz: 'UTC',
      bucket: 'day',
    })
    expect(wrapper.find('.v-main h1').text()).toBe('Statistics')
    expect(wrapper.find('.stats-tiles').exists()).toBe(true)
    // The driving's view by default: three charts by interval, then the trips by
    // distance and what each band consumed.
    expect(wrapper.findAll('.chart-frame')).toHaveLength(5)
    expect(wrapper.find('.view-toggle [aria-pressed="true"]').text()).toBe('Driving')
    expect(wrapper.find('.v-tab[aria-current="page"]').text()).toBe('Stats')
  })

  it('shows one view of the charts at a time, kept in the URL with the period', async () => {
    const { wrapper, router } = await page('?from=2026-09-01&to=2026-09-30&view=charging')
    const titles = () => wrapper.findAll('.chart-frame h2').map((h) => h.text())
    expect(titles()).toEqual([
      'Energy charged (from the state of charge)',
      'Loss while parked',
      'State of charge at the start and end of charges',
      'Energy charged by place (from the state of charge)',
    ])
    const toggle = wrapper.find('.view-toggle')
    expect(toggle.attributes('role')).toBe('group')
    expect(toggle.attributes('aria-label')).toBe('Charts')
    await toggle.findAll('button')[2]?.trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query.view).toBe('costs'))
    expect(router.currentRoute.value.query.from).toBe('2026-09-01')
    await flushPromises()
    expect(titles()).toEqual(['Cost of charges', 'Cost per 100 km', 'Cost of charges by place'])
    // A view the page does not know is the driving's.
    await router.replace({ query: { ...router.currentRoute.value.query, view: 'nope' } })
    await flushPromises()
    expect(titles()[0]).toBe('Distance')
  })

  it('tells why the costs have no chart: no currency, or not in the offer', async () => {
    mocks.fetchSettings.mockResolvedValue(unset)
    let { wrapper } = await page('?from=2026-09-01&to=2026-09-30&view=costs')
    expect(wrapper.findAll('.chart-frame')).toHaveLength(0)
    expect(wrapper.find('.v-alert').text()).toContain('No currency chosen yet')
    expect(wrapper.find('a.choose-currency').attributes('href')).toBe('/settings')
    ;({ wrapper } = await page('?from=2026-09-01&to=2026-09-30&view=costs', ['costs']))
    expect(wrapper.find('.feature-unavailable').exists()).toBe(true)
    expect(wrapper.find('a.choose-currency').exists()).toBe(false)
  })

  it("shows the extension's component instead of the costs' message, when it has one", async () => {
    extension.costsUnavailable = { render: () => h('div', { class: 'extension-costs' }, 'Sample') }
    const { wrapper } = await page('?from=2026-09-01&to=2026-09-30&view=costs', ['costs'])
    expect(wrapper.find('.extension-costs').text()).toBe('Sample')
    expect(wrapper.find('.feature-unavailable').exists()).toBe(false)
  })

  it('reads the period and the split of the URL', async () => {
    await page('?from=2025-10-01&to=2026-09-30&bucket=week')
    expect(mocks.fetchStats).toHaveBeenCalledWith(vehicle, {
      from: '2025-10-01T00:00:00.000Z',
      to: '2026-10-01T00:00:00.000Z',
      tz: 'UTC',
      bucket: 'week',
    })
  })

  it('never sends an inverted period', async () => {
    const { wrapper } = await page('?from=2026-09-30&to=2026-09-01')
    expect(mocks.fetchStats).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('The end comes before the start.')
  })

  it('keeps the period on the way to the lists', async () => {
    const { wrapper } = await page('?from=2026-09-01&to=2026-09-30')
    const trips = wrapper.findAll('.v-tab').find((t) => t.text() === 'Trips')
    expect(trips?.attributes('href')).toBe(
      `/vehicles/${vehicle}/trips?from=2026-09-01&to=2026-09-30`,
    )
  })

  it("says the statistics are part of another offer, where the account's leaves them out", async () => {
    const { wrapper, router } = await page('', ['stats'])
    expect(wrapper.find('.feature-unavailable').text()).toContain(i18n.global.t('limits.stats'))
    expect(wrapper.find('.stats-tiles').exists()).toBe(false)
    expect(mocks.fetchStats).not.toHaveBeenCalled()
    expect(router.currentRoute.value.query).toEqual({})
  })
})
