import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { VehiclePage } from '@/pages/vehicle'
import { ApiError } from '@/shared/api'
import { RecentSummary } from '@/widgets/recent-summary'
import empty from '@fixtures/stats-empty.json'
import state from '@fixtures/state.json'
import reauth from '@fixtures/vehicle-reauth-required.json'
import unknown from '@fixtures/state-unknown.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

const mocks = vi.hoisted(() => ({
  fetchVehicles: vi.fn(),
  fetchVehicle: vi.fn(),
  fetchVehicleState: vi.fn(),
}))
// The charts have their own tests: here, only that the page loads them. The summary of
// the last 30 days gets no event, and shows nothing.
vi.mock('@/widgets/soc-timeline', () => ({
  SocTimeline: defineComponent({
    name: 'SocTimeline',
    props: { vehicle: { type: String, required: true } },
    render: () => null,
  }),
}))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats: () => Promise.resolve(empty),
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  ...mocks,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
})

async function page(vehicle: string) {
  const router = testRouter([
    { path: '/vehicles/:vehicle', name: 'vehicle', component: { render: () => null } },
  ])
  await router.push(`/vehicles/${vehicle}`)
  const { wrapper } = mountWith(VehiclePage, { router, props: { vehicle } })
  await flushPromises()
  return wrapper
}

describe('VehiclePage', () => {
  it('shows the vehicle, its state and the selector', async () => {
    mocks.fetchVehicle.mockResolvedValue(reauth)
    mocks.fetchVehicleState.mockResolvedValue(unknown)
    const w = await page(reauth.id)
    // Its details were never read: the VIN is the title, and not repeated.
    expect(w.find('h1').text()).toBe(reauth.vin)
    expect(w.findAll('.vin')).toHaveLength(1)
    expect(w.text()).toContain('Re-authentication required')
    expect(w.findComponent({ name: 'VSelect' }).exists()).toBe(true)
    expect(mocks.fetchVehicleState).toHaveBeenCalledWith(reauth.id)
  })

  it('has no selector with a single vehicle', async () => {
    mocks.fetchVehicles.mockResolvedValue([vehicles.items[0]])
    mocks.fetchVehicle.mockResolvedValue(vehicles.items[0])
    mocks.fetchVehicleState.mockResolvedValue(state)
    const w = await page(state.vehicle_id)
    expect(w.findComponent({ name: 'VSelect' }).exists()).toBe(false)
    expect(w.find('h1').text()).toBe('XC40 Recharge Twin · 2021')
    expect(w.find('span.vin').text()).toBe(vehicles.items[0]?.vin)
    expect(w.text()).toContain('64%')
    expect(w.findComponent({ name: 'SocTimeline' }).props('vehicle')).toBe(state.vehicle_id)
    expect(w.findComponent(RecentSummary).props('vehicle')).toBe(state.vehicle_id)
  })

  it("leads to the vehicle's own settings", async () => {
    mocks.fetchVehicle.mockResolvedValue(vehicles.items[0])
    mocks.fetchVehicleState.mockResolvedValue(state)
    const w = await page(state.vehicle_id)
    const link = w.find('a.vehicle-settings')
    expect(link.text()).toBe('Vehicle settings')
    expect(link.attributes('href')).toBe(`/vehicles/${state.vehicle_id}/settings`)
  })

  it('tells an unknown vehicle, with a way back', async () => {
    const e = new ApiError(404, 'not_found', '')
    mocks.fetchVehicle.mockRejectedValue(e)
    mocks.fetchVehicleState.mockRejectedValue(e)
    const w = await page('nope')
    expect(w.find('.v-alert').text()).toBe('This vehicle does not exist.')
    expect(w.findAll('.v-alert')).toHaveLength(1)
    expect(w.find('.v-main a[href="/"]').text()).toBe('Back to my vehicles')
  })
})
