import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  collectionRefetchInterval,
  useCollection,
  useCollections,
} from '@/features/watch-collection'
import vehicles from '@fixtures/vehicles.json'
import { withSetup } from '@test/utils'

const mocks = vi.hoisted(() => ({ fetchVehicle: vi.fn(), fetchVehicles: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicle: mocks.fetchVehicle,
  fetchVehicles: mocks.fetchVehicles,
}))

const car = vehicles.items[0]

beforeEach(() => {
  mocks.fetchVehicle.mockReset()
  mocks.fetchVehicles.mockReset()
  mocks.fetchVehicle.mockResolvedValue(car)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
})
afterEach(() => {
  vi.useRealTimers()
})

describe('useCollection', () => {
  it('reads the vehicle', async () => {
    const { result } = withSetup(() => useCollection(car?.id ?? ''))
    await flushPromises()
    expect(result.vehicle.value).toEqual(car)
    expect(mocks.fetchVehicle).toHaveBeenCalledWith(car?.id)
  })

  it('refreshes every minute', async () => {
    vi.useFakeTimers()
    withSetup(() => useCollection(car?.id ?? ''))
    await vi.advanceTimersByTimeAsync(0)
    expect(collectionRefetchInterval).toBe(60_000)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(mocks.fetchVehicle).toHaveBeenCalledTimes(2)
  })
})

describe('useCollections', () => {
  it('reads every vehicle, and refreshes them every minute', async () => {
    vi.useFakeTimers()
    const { result } = withSetup(() => useCollections())
    await vi.advanceTimersByTimeAsync(0)
    expect(result.vehicles.value).toEqual(vehicles.items)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(mocks.fetchVehicles).toHaveBeenCalledTimes(2)
  })
})
