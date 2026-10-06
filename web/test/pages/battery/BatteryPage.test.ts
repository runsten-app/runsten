import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BatteryPage } from '@/pages/battery'
import battery from '@fixtures/battery.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const mocks = vi.hoisted(() => ({ getBattery: vi.fn(), fetchVehicles: vi.fn() }))
vi.mock('@/entities/battery', async (original) => ({
  ...(await original<typeof import('@/entities/battery')>()),
  getBattery: mocks.getBattery,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.getBattery.mockResolvedValue(battery)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
})

const vehicle = vehicles.items[0]?.id ?? ''

describe('BatteryPage', () => {
  it('shows the two sentences of honesty, the figures and the two charts', async () => {
    const { wrapper } = mountWith(BatteryPage, { props: { vehicle } })
    await flushPromises()
    expect(wrapper.find('.v-main h1').text()).toBe('Battery')
    // Where the estimate comes from, what it is not, what to read in it.
    expect(wrapper.find('.intro').text()).toContain("Runsten estimates your battery's capacity")
    expect(wrapper.find('.intro').text()).toContain('A single estimate is approximate')
    expect(wrapper.find('.battery-tiles').exists()).toBe(true)
    expect(wrapper.findAll('.chart-frame')).toHaveLength(2)
  })

  it('keeps the vehicle shell, its own tab current', async () => {
    const router = testRouter()
    await router.push(`/vehicles/${vehicle}/battery`)
    const { wrapper } = mountWith(BatteryPage, { router, props: { vehicle } })
    await flushPromises()
    expect(wrapper.findAll('.v-tab')).toHaveLength(5)
    expect(wrapper.find('.v-tab[aria-current="page"]').text()).toBe('Battery')
    expect(wrapper.findComponent({ name: 'CollectionNotice' }).exists()).toBe(true)
  })
})
