import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { stateRefetchInterval, useVehicleState } from '@/features/watch-vehicle-state'
import state from '@fixtures/state.json'
import { withSetup } from '@test/utils'

const { fetchVehicleState } = vi.hoisted(() => ({ fetchVehicleState: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicleState,
}))

beforeEach(() => {
  fetchVehicleState.mockReset()
  fetchVehicleState.mockResolvedValue(state)
})
afterEach(() => {
  vi.useRealTimers()
})

describe('useVehicleState', () => {
  it('reads the state of the vehicle', async () => {
    const { result } = withSetup(() => useVehicleState(state.vehicle_id))
    await flushPromises()
    expect(result.state.value).toEqual(state)
    expect(fetchVehicleState).toHaveBeenCalledWith(state.vehicle_id)
  })

  it('refreshes every minute, and stops with its component', async () => {
    vi.useFakeTimers()
    const { wrapper } = withSetup(() => useVehicleState(state.vehicle_id))
    await vi.advanceTimersByTimeAsync(0)
    expect(fetchVehicleState).toHaveBeenCalledTimes(1)
    expect(stateRefetchInterval).toBe(60_000)
    await vi.advanceTimersByTimeAsync(59_999)
    expect(fetchVehicleState).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(fetchVehicleState).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(fetchVehicleState).toHaveBeenCalledTimes(3)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(5 * 60_000)
    expect(fetchVehicleState).toHaveBeenCalledTimes(3)
  })

  it('pauses while the tab is hidden', async () => {
    vi.useFakeTimers()
    const hidden = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    withSetup(() => useVehicleState(state.vehicle_id))
    await vi.advanceTimersByTimeAsync(0)
    const calls = fetchVehicleState.mock.calls.length
    await vi.advanceTimersByTimeAsync(3 * 60_000)
    expect(fetchVehicleState).toHaveBeenCalledTimes(calls)
    hidden.mockRestore()
  })
})
