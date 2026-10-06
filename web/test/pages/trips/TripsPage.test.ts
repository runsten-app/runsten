import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { TripsPage } from '@/pages/trips'
import page2 from '@fixtures/trips-page-2.json'
import totals from '@fixtures/stats-totals.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

// The chart has its own tests: here, only that the page loads it, on its period.
vi.mock('@/widgets/period-chart', () => ({
  PeriodChart: defineComponent({
    name: 'PeriodChart',
    props: {
      vehicle: { type: String, required: true },
      kind: { type: String, required: true },
      from: { type: String, default: undefined },
      to: { type: String, default: undefined },
      days: { type: Object, required: true },
    },
    render: () => null,
  }),
}))
const mocks = vi.hoisted(() => ({
  fetchTrips: vi.fn(),
  fetchVehicles: vi.fn(),
  fetchStats: vi.fn(),
}))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  fetchTrips: mocks.fetchTrips,
}))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats: mocks.fetchStats,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchTrips.mockResolvedValue(page2)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
  mocks.fetchStats.mockResolvedValue(totals)
})

const vehicle = vehicles.items[0]?.id ?? ''

async function page(query = '') {
  const router = testRouter()
  await router.push(`/vehicles/${vehicle}/trips${query}`)
  const { wrapper } = mountWith(TripsPage, { router, props: { vehicle } })
  await flushPromises()
  return { wrapper, router }
}

describe('TripsPage', () => {
  it('reads the trips of the period of the URL', async () => {
    const { wrapper } = await page('?from=2026-09-01&to=2026-09-30')
    expect(wrapper.find('.v-main h1').text()).toBe('Trips')
    // The period in one line, as on the statistics page.
    expect(wrapper.find('.period-picker .current').text()).toBe('Period: September 2026')
    expect(wrapper.findComponent({ name: 'PeriodChart' }).props()).toEqual({
      vehicle,
      kind: 'trips',
      from: '2026-09-01T00:00:00.000Z',
      to: '2026-10-01T00:00:00.000Z',
      days: { from: '2026-09-01', to: '2026-09-30' },
    })
    expect(mocks.fetchTrips).toHaveBeenCalledWith(vehicle, {
      from: '2026-09-01T00:00:00.000Z',
      to: '2026-10-01T00:00:00.000Z',
      cursor: undefined,
    })
    expect(wrapper.findAll('.trip-summary')).toHaveLength(1)
    // The totals of the same period, above the list.
    expect(mocks.fetchStats).toHaveBeenCalledWith(vehicle, {
      from: '2026-09-01T00:00:00.000Z',
      to: '2026-10-01T00:00:00.000Z',
      tz: 'UTC',
      bucket: undefined,
    })
    expect(wrapper.find('.period-totals').text()).toMatch(/^3 trips started in the period/)
    // The CSV file of the same period, beside the title.
    expect(wrapper.find('a.csv-download').attributes('href')).toBe(
      `api/v1/vehicles/${vehicle}/trips.csv?from=2026-09-01T00%3A00%3A00.000Z&to=2026-10-01T00%3A00%3A00.000Z`,
    )
  })

  it('reads them again when the period changes the URL', async () => {
    const { wrapper, router } = await page()
    // No period: the whole history, and no period before or after it.
    expect(wrapper.find('.period-picker .current').text()).toBe('Period: Whole history')
    expect(wrapper.find('.period-picker .previous').exists()).toBe(false)
    expect(mocks.fetchTrips).toHaveBeenLastCalledWith(vehicle, {
      from: undefined,
      to: undefined,
      cursor: undefined,
    })
    await router.replace({ query: { to: '2026-09-28' } })
    await flushPromises()
    expect(mocks.fetchTrips).toHaveBeenLastCalledWith(vehicle, {
      from: undefined,
      to: '2026-09-29T00:00:00.000Z',
      cursor: undefined,
    })
  })

  it('never sends an inverted period', async () => {
    const { wrapper } = await page('?from=2026-09-30&to=2026-09-01')
    expect(mocks.fetchTrips).not.toHaveBeenCalled()
    expect(mocks.fetchStats).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('The end comes before the start.')
    expect(wrapper.find('.trip-list').exists()).toBe(false)
    expect(wrapper.find('.csv-download').exists()).toBe(false)
  })

  it('leads to the charges with the same period', async () => {
    const { wrapper } = await page('?from=2026-09-01')
    const tab = wrapper.findAll('.v-tab').find((t) => t.text() === 'Charges')
    expect(tab?.attributes('href')).toBe(`/vehicles/${vehicle}/charges?from=2026-09-01`)
    const current = wrapper.find('.v-tab[aria-current="page"]')
    expect(current.text()).toBe('Trips')
  })

  // Links between pages, each reached with Tab: not the tablist of v-tabs, with its
  // roving tabindex.
  it('leads to the sections with plain links, the current one aria-current', async () => {
    const { wrapper } = await page()
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    const links = wrapper.findAll('nav a.v-tab')
    expect(links.map((l) => l.text())).toEqual(['State', 'Battery', 'Trips', 'Charges', 'Stats'])
    for (const l of links) {
      expect(l.attributes('role')).toBeUndefined()
      expect(l.attributes('tabindex')).toBeUndefined()
      expect(l.attributes('aria-selected')).toBeUndefined()
    }
    expect(links.map((l) => l.attributes('aria-current'))).toEqual([
      undefined,
      undefined,
      'page',
      undefined,
      undefined,
    ])
  })
})
