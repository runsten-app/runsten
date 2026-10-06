import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useVehicle, useVehicles } from '@/features/select-vehicle'
import { ApiError } from '@/shared/api'
import reauth from '@fixtures/vehicle-reauth-required.json'
import vehicles from '@fixtures/vehicles.json'
import { withSetup } from '@test/utils'

const { fetchVehicles, fetchVehicle } = vi.hoisted(() => ({
  fetchVehicles: vi.fn(),
  fetchVehicle: vi.fn(),
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles,
  fetchVehicle,
}))

beforeEach(() => {
  fetchVehicles.mockReset()
  fetchVehicle.mockReset()
})

describe('useVehicles', () => {
  it('lists the vehicles', async () => {
    fetchVehicles.mockResolvedValue(vehicles.items)
    const { result } = withSetup(useVehicles)
    expect(result.isPending.value).toBe(true)
    await flushPromises()
    expect(result.vehicles.value).toEqual(vehicles.items)
  })
})

describe('useVehicle', () => {
  it('reads the vehicle, and follows its ID', async () => {
    fetchVehicle.mockResolvedValue(reauth)
    const id = { value: reauth.id }
    const { result } = withSetup(() => useVehicle(() => id.value))
    await flushPromises()
    expect(result.vehicle.value).toEqual(reauth)
    expect(fetchVehicle).toHaveBeenCalledWith(reauth.id)
  })

  it('reports an unknown vehicle', async () => {
    fetchVehicle.mockRejectedValue(new ApiError(404, 'not_found', ''))
    const { result } = withSetup(() => useVehicle('nope'))
    await flushPromises()
    expect(result.error.value).toMatchObject({ code: 'not_found' })
  })
})
