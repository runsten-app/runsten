import { beforeEach, describe, expect, it, vi } from 'vitest'
import { chargeKeys } from '@/entities/charge'
import { placeKeys } from '@/entities/place'
import { statsKeys } from '@/entities/stats'
import { useDeleteChargeCost, useSetChargeCost } from '@/features/enter-charge-cost'
import entered from '@fixtures/charge-cost.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({ setChargeCost: vi.fn(), deleteChargeCost: vi.fn() }))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

// What an entered cost makes stale: the charges, the orphans, the statistics.
const stale = [
  chargeKeys.all(),
  chargeKeys.details(),
  chargeKeys.orphans(),
  placeKeys.unpricedAll(),
  statsKeys.every(),
]
const keys = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)

describe('useSetChargeCost', () => {
  it('enters a cost, keeps the charge it gives back, and refreshes what it changes', async () => {
    api.setChargeCost.mockResolvedValue(entered)
    const { result, queryClient } = withSetup(useSetChargeCost)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const cost = { amount_minor: 850, energy_kwh: null, note: null }
    expect(await result.setChargeCost({ vehicle: 'v1', id: entered.id, cost })).toEqual(entered)
    expect(api.setChargeCost).toHaveBeenCalledWith('v1', entered.id, cost)
    expect(queryClient.getQueryData(chargeKeys.detail('v1', entered.id))).toEqual(entered)
    expect(keys(invalidate)).toEqual(stale)
  })
})

describe('useDeleteChargeCost', () => {
  it('deletes an entered cost and refreshes what it changes', async () => {
    api.deleteChargeCost.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(useDeleteChargeCost)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.deleteChargeCost({ vehicle: 'v1', id: entered.id })
    expect(api.deleteChargeCost).toHaveBeenCalledWith('v1', entered.id)
    expect(keys(invalidate)).toEqual(stale)
  })
})
