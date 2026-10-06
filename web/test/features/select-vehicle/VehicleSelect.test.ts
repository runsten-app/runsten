import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { VehicleSelect } from '@/features/select-vehicle'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

const { fetchVehicles } = vi.hoisted(() => ({ fetchVehicles: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles,
}))

beforeEach(() => {
  fetchVehicles.mockReset()
})

type Item = (typeof vehicles.items)[0]
const [first, second, third] = vehicles.items as [Item, Item, Item]

function router() {
  return testRouter([
    { path: '/vehicles/:vehicle', name: 'vehicle', component: { render: () => null } },
  ])
}

describe('VehicleSelect', () => {
  it('lists the vehicles by their label and changes the route', async () => {
    fetchVehicles.mockResolvedValue(vehicles.items)
    const r = router()
    await r.push(`/vehicles/${first.id}`)
    const { wrapper } = mountWith(VehicleSelect, { router: r, props: { vehicle: first.id } })
    await flushPromises()
    const select = wrapper.findComponent({ name: 'VSelect' })
    expect(select.props('items')).toEqual([
      { value: first.id, title: 'XC40 Recharge Twin · 2021' },
      { value: second.id, title: second.vin }, // details never read
      { value: third.id, title: 'EX30 Single Motor Extended Range · 2024' }, // chosen
    ])
    expect(select.props('modelValue')).toBe(first.id)
    select.vm.$emit('update:modelValue', second.id)
    await vi.waitFor(() => expect(r.currentRoute.value.params.vehicle).toBe(second.id))
  })

  it.each([
    ['/trips?from=2026-09-01', 'trips'],
    ['/trips/2026-09-28T07:01:00.000000Z?from=2026-09-01', 'trips'],
    ['/charges/x?from=2026-09-01', 'charges'],
  ])('stays on the same list from %s, with its period', async (rest, list) => {
    fetchVehicles.mockResolvedValue(vehicles.items)
    const r = testRouter()
    await r.push(`/vehicles/${first.id}${rest}`)
    const { wrapper } = mountWith(VehicleSelect, { router: r, props: { vehicle: first.id } })
    await flushPromises()
    wrapper.findComponent({ name: 'VSelect' }).vm.$emit('update:modelValue', second.id)
    await vi.waitFor(() =>
      expect(r.currentRoute.value.fullPath).toBe(`/vehicles/${second.id}/${list}?from=2026-09-01`),
    )
  })

  it('tells apart two vehicles of the same model by their VIN', async () => {
    fetchVehicles.mockResolvedValue([first, { ...first, id: second.id, vin: 'YV1SMLT0000DT0742' }])
    const { wrapper } = mountWith(VehicleSelect, { router: router(), props: { vehicle: first.id } })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'VSelect' }).props('items')).toEqual([
      { value: first.id, title: 'XC40 Recharge Twin · 2021 · DT0001' },
      { value: second.id, title: 'XC40 Recharge Twin · 2021 · DT0742' },
    ])
  })

  it('stays where it is on the same vehicle', async () => {
    fetchVehicles.mockResolvedValue(vehicles.items)
    const r = router()
    await r.push(`/vehicles/${first.id}`)
    const push = vi.spyOn(r, 'push')
    const { wrapper } = mountWith(VehicleSelect, { router: r, props: { vehicle: first.id } })
    await flushPromises()
    wrapper.findComponent({ name: 'VSelect' }).vm.$emit('update:modelValue', first.id)
    expect(push).not.toHaveBeenCalled()
  })

  it('is hidden with a single vehicle', async () => {
    fetchVehicles.mockResolvedValue([first])
    const { wrapper } = mountWith(VehicleSelect, { router: router(), props: { vehicle: first.id } })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'VSelect' }).exists()).toBe(false)
  })
})
