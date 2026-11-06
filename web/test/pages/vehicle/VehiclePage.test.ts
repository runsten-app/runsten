import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { VehiclePage } from '@/pages/vehicle'
import { ApiError } from '@/shared/api'
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
// The chart has its own tests: here, only that the page loads it.
vi.mock('@/widgets/soc-timeline', () => ({
  SocTimeline: defineComponent({
    name: 'SocTimeline',
    props: { vehicle: { type: String, required: true } },
    render: () => null,
  }),
}))
// The extensions' actions: none in the public build, one in a test.
const { actions } = vi.hoisted(() => ({ actions: [] as unknown[] }))
vi.mock('@/pages/vehicle/extensions', () => ({ extensionVehicleActions: actions }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  ...mocks,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  actions.length = 0
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
  it('shows the vehicle, its state and its menu', async () => {
    mocks.fetchVehicle.mockResolvedValue(reauth)
    mocks.fetchVehicleState.mockResolvedValue(unknown)
    const w = await page(reauth.id)
    // Its details were never read: the VIN is the title.
    expect(w.find('h1').text()).toBe(reauth.vin)
    expect(w.text()).toContain('Re-authentication required')
    expect(w.find('button.vehicle-menu').exists()).toBe(true)
    expect(mocks.fetchVehicleState).toHaveBeenCalledWith(reauth.id)
  })

  it('is named in the bar with a single vehicle too', async () => {
    mocks.fetchVehicles.mockResolvedValue([vehicles.items[0]])
    mocks.fetchVehicle.mockResolvedValue(vehicles.items[0])
    mocks.fetchVehicleState.mockResolvedValue(state)
    const w = await page(state.vehicle_id)
    expect(w.find('button.vehicle-menu').text()).toBe('XC40 Recharge Twin · 2021')
    // The bar's picker names it: the heading is for screen readers only, without the VIN.
    expect(w.find('h1').text()).toBe('XC40 Recharge Twin · 2021')
    expect(w.find('h1').classes()).toContain('d-sr-only')
    expect(w.text()).not.toContain(vehicles.items[0]?.vin)
    expect(w.text()).toContain('64%')
    expect(w.findComponent({ name: 'SocTimeline' }).props('vehicle')).toBe(state.vehicle_id)
    // The last days at the foot of the state's grid.
    expect(w.find('.verdandi').findComponent({ name: 'SocTimeline' }).exists()).toBe(true)
    // The totals of a longer period are the statistics', not the state's.
    expect(w.find('.recent-summary').exists()).toBe(false)
  })

  // The bar's menu leads to the vehicle's settings: the page repeats no way there.
  it("leaves the way to the vehicle's settings to the bar", async () => {
    mocks.fetchVehicle.mockResolvedValue(vehicles.items[0])
    mocks.fetchVehicleState.mockResolvedValue(state)
    const w = await page(state.vehicle_id)
    expect(w.find('.v-main a[href$="/settings"]').exists()).toBe(false)
  })

  it("shows an extension's actions after the state, given the vehicle", async () => {
    mocks.fetchVehicle.mockResolvedValue(vehicles.items[0])
    mocks.fetchVehicleState.mockResolvedValue(state)
    expect((await page(state.vehicle_id)).find('.vehicle-action').exists()).toBe(false)

    actions.push(
      defineComponent({
        name: 'VehicleAction',
        props: { vehicle: { type: String, required: true } },
        render: () => h('section', { class: 'vehicle-action' }),
      }),
    )
    const w = await page(state.vehicle_id)
    expect(w.findComponent({ name: 'VehicleAction' }).props('vehicle')).toBe(state.vehicle_id)
    // After the state; the summary, which has no event here, shows nothing.
    const html = w.html()
    expect(html.indexOf('verdandi')).toBeLessThan(html.indexOf('vehicle-action'))
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
