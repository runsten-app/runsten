import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { ChargesPage } from '@/pages/charges'
import charges from '@fixtures/charges.json'
import totals from '@fixtures/stats-totals.json'
import vehicles from '@fixtures/vehicles.json'
import { sessionKeys } from '@/entities/session'
import session from '@fixtures/session.json'
import { mountWith, testQueryClient, testRouter } from '@test/utils'

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
  fetchCharges: vi.fn(),
  fetchVehicles: vi.fn(),
  fetchStats: vi.fn(),
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchCharges: mocks.fetchCharges,
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
  mocks.fetchCharges.mockResolvedValue(charges)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
  mocks.fetchStats.mockResolvedValue(totals)
})

const vehicle = vehicles.items[0]?.id ?? ''

describe('ChargesPage', () => {
  it('shows the charges of the period, and leads back to the trips with it', async () => {
    const router = testRouter()
    await router.push(`/vehicles/${vehicle}/charges?to=2026-09-28`)
    const { wrapper } = mountWith(ChargesPage, { router, props: { vehicle } })
    await flushPromises()
    expect(wrapper.find('.v-main h1').text()).toBe('Charges')
    expect(wrapper.findComponent({ name: 'PeriodChart' }).props()).toMatchObject({
      kind: 'charges',
      to: '2026-09-29T00:00:00.000Z',
      days: { to: '2026-09-28' },
    })
    expect(mocks.fetchCharges).toHaveBeenCalledWith(vehicle, {
      from: undefined,
      to: '2026-09-29T00:00:00.000Z',
      cursor: undefined,
    })
    expect(wrapper.findAll('.charge-summary')).toHaveLength(2)
    expect(wrapper.find('.period-totals').text()).toMatch(/^2 charges started in the period/)
    const tabs = wrapper.findAll('.v-tab')
    expect(tabs.map((t) => t.attributes('href'))).toEqual([
      `/vehicles/${vehicle}`,
      `/vehicles/${vehicle}/battery`,
      `/vehicles/${vehicle}/trips?to=2026-09-28`,
      `/vehicles/${vehicle}/charges?to=2026-09-28`,
      `/vehicles/${vehicle}/stats?to=2026-09-28`,
    ])
    expect(wrapper.find('.v-tab[aria-current="page"]').text()).toBe('Charges')
    expect(wrapper.find('a.csv-download').attributes('href')).toBe(
      `api/v1/vehicles/${vehicle}/charges.csv?to=2026-09-29T00%3A00%3A00.000Z`,
    )
  })

  it('offers no CSV file to an account whose offer leaves it out', async () => {
    const router = testRouter()
    await router.push(`/vehicles/${vehicle}/charges`)
    const queryClient = testQueryClient()
    queryClient.setQueryData(sessionKeys.current(), {
      ...session,
      limits: { history_from: null, unavailable: ['csv'], unread_vehicles: [] },
    })
    const { wrapper } = mountWith(ChargesPage, { router, queryClient, props: { vehicle } })
    await flushPromises()
    expect(wrapper.findAll('.charge-summary')).toHaveLength(2)
    expect(wrapper.find('.csv-download').exists()).toBe(false)
  })
})
