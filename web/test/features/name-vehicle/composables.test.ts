import { beforeEach, describe, expect, it, vi } from 'vitest'
import { vehicleKeys } from '@/entities/vehicle'
import { useSetVehicleAlias } from '@/features/name-vehicle'
import named from '@fixtures/vehicle-alias.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({ setVehicleAlias: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  ...api,
}))

beforeEach(() => {
  api.setVehicleAlias.mockReset()
})

describe('useSetVehicleAlias', () => {
  it('writes the alias, caches the vehicle and refreshes the list and the detail', async () => {
    api.setVehicleAlias.mockResolvedValue(named)
    const { result, queryClient } = withSetup(useSetVehicleAlias)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const alias = { alias: 'Fleet car 12' }
    expect(await result.setVehicleAlias({ vehicle: named.id, alias })).toEqual(named)
    expect(api.setVehicleAlias).toHaveBeenCalledWith(named.id, alias)
    expect(queryClient.getQueryData(vehicleKeys.detail(named.id))).toEqual(named)
    expect(invalidate.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)).toEqual([
      vehicleKeys.list(),
      vehicleKeys.detail(named.id),
    ])
  })
})
