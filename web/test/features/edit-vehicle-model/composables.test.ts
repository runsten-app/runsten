import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { chargeKeys } from '@/entities/charge'
import { statsKeys } from '@/entities/stats'
import { vehicleKeys } from '@/entities/vehicle'
import { useSetVehicleModel, useVariants } from '@/features/edit-vehicle-model'
import variants from '@fixtures/variants.json'
import chosen from '@fixtures/vehicle-model.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({ fetchVariants: vi.fn(), setVehicleModel: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

describe('useVariants', () => {
  it('reads the variants of a vehicle', async () => {
    api.fetchVariants.mockResolvedValue(variants.items)
    const { result } = withSetup(() => useVariants(chosen.id))
    await flushPromises()
    expect(api.fetchVariants).toHaveBeenCalledWith(chosen.id)
    expect(result.variants.value).toEqual(variants.items)
  })
})

describe('useSetVehicleModel', () => {
  it('writes the choice, caches the vehicle and refreshes the list and the costs', async () => {
    api.setVehicleModel.mockResolvedValue(chosen)
    const { result, queryClient } = withSetup(useSetVehicleModel)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const choice = { variant_id: 'ex30-er-2024', ac_max_kw: 11 }
    expect(await result.setVehicleModel({ vehicle: chosen.id, choice })).toEqual(chosen)
    expect(api.setVehicleModel).toHaveBeenCalledWith(chosen.id, choice)
    expect(queryClient.getQueryData(vehicleKeys.detail(chosen.id))).toEqual(chosen)
    // The charger bounds the costs at once.
    expect(invalidate.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)).toEqual([
      vehicleKeys.list(),
      chargeKeys.all(),
      chargeKeys.details(),
      statsKeys.every(),
    ])
  })
})
