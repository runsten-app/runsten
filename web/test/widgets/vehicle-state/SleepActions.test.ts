import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { VehicleState } from '@/widgets/vehicle-state'
import state from '@fixtures/state.json'
import vehicle from '@fixtures/vehicle-model.json'
import { mountWith } from '@test/utils'

const { fetchVehicle, fetchVehicleState } = vi.hoisted(() => ({
  fetchVehicle: vi.fn(),
  fetchVehicleState: vi.fn(),
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicle,
  fetchVehicleState,
}))
// A downstream build's action, given the vehicle and how it sleeps.
vi.mock('@/widgets/vehicle-state/extensions', async () => {
  const { defineComponent, h } = await import('vue')
  const Wake = defineComponent({
    props: { vehicle: { type: String, required: true }, sleep: { type: String, required: true } },
    setup: (p) => () => h('button', { class: 'wake' }, `${p.vehicle} ${p.sleep}`),
  })
  return { extensionSleepActions: [Wake] }
})

describe('VehicleState with sleep actions', () => {
  it("puts an extension's actions in the band of a car asleep, none for one awake", async () => {
    fetchVehicle.mockResolvedValue(vehicle)
    const at = {
      reported_at: null,
      fetched_at: '2026-09-28T18:40:00Z',
      checked_at: '2026-09-28T19:06:00Z',
    }
    fetchVehicleState.mockResolvedValue({ ...state, sleep: { ...at, value: 'asleep' } })
    let { wrapper: w } = mountWith(VehicleState, { props: { vehicle: state.vehicle_id } })
    await flushPromises()
    expect(w.find('.headline .sleep-actions .wake').text()).toBe(`${state.vehicle_id} asleep`)
    w.unmount()
    fetchVehicleState.mockResolvedValue({ ...state, sleep: { ...at, value: 'awake' } })
    ;({ wrapper: w } = mountWith(VehicleState, { props: { vehicle: state.vehicle_id } }))
    await flushPromises()
    expect(w.find('.wake').exists()).toBe(false)
  })
})
