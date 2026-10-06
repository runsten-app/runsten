import { beforeEach, describe, expect, it, vi } from 'vitest'
import { chargeKeys } from '@/entities/charge'
import { placeKeys } from '@/entities/place'
import { statsKeys } from '@/entities/stats'
import { useAttachOrphanCost, useDeleteOrphanCost } from '@/features/manage-orphan-costs'
import charge from '@fixtures/charge.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({ attachOrphanCost: vi.fn(), deleteOrphanCost: vi.fn() }))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

// What attaching or deleting an orphan makes stale.
const stale = [
  chargeKeys.orphans(),
  chargeKeys.all(),
  chargeKeys.details(),
  chargeKeys.allCandidates(),
  placeKeys.unpricedAll(),
  statsKeys.every(),
]
const keys = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)

describe('orphaned costs', () => {
  it('attaches one to a charge, and refreshes what it changes', async () => {
    api.attachOrphanCost.mockResolvedValue(charge)
    const { result, queryClient } = withSetup(useAttachOrphanCost)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.attachOrphanCost({ orphan: 'o', vehicle: 'v1', charge: charge.id })
    expect(api.attachOrphanCost).toHaveBeenCalledWith('o', 'v1', charge.id)
    expect(keys(invalidate)).toEqual(stale)
  })

  it('deletes one, and refreshes what it changes', async () => {
    api.deleteOrphanCost.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(useDeleteOrphanCost)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.deleteOrphanCost('o')
    expect(api.deleteOrphanCost).toHaveBeenCalledWith('o')
    expect(keys(invalidate)).toEqual(stale)
  })
})
