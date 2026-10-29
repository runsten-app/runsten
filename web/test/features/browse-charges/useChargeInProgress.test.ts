import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { chargeKeys } from '@/entities/charge'
import { statsKeys } from '@/entities/stats'
import { useCharge, useChargeInProgress } from '@/features/browse-charges'
import charging from '@fixtures/charging.json'
import { QueryClient } from '@tanstack/vue-query'
import { withSetup } from '@test/utils'

const mocks = vi.hoisted(() => ({ fetchChargeInProgress: vi.fn(), fetchCharge: vi.fn() }))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  ...mocks,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
})

const vehicle = 'v1'
const client = () => new QueryClient({ defaultOptions: { queries: { retry: false } } })

describe('useChargeInProgress', () => {
  it('reads the charge in progress', async () => {
    mocks.fetchChargeInProgress.mockResolvedValue(charging.charge)
    const { result } = withSetup(() => useChargeInProgress(vehicle))
    await flushPromises()
    expect(mocks.fetchChargeInProgress).toHaveBeenCalledWith(vehicle)
    expect(result.charge.value?.id).toBe(charging.charge?.id)
  })

  // Over, it is a charge of the lists: they, its details and the statistics are read
  // again.
  it('reads the lists again once it ends', async () => {
    mocks.fetchChargeInProgress.mockResolvedValue(charging.charge)
    const queryClient = client()
    withSetup(() => useChargeInProgress(vehicle), { queryClient })
    await flushPromises()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    mocks.fetchChargeInProgress.mockResolvedValue(null)
    await queryClient.refetchQueries({ queryKey: chargeKeys.inProgress(vehicle) })
    await flushPromises()
    expect(invalidate.mock.calls.map(([f]) => f)).toEqual([
      { queryKey: chargeKeys.lists(vehicle) },
      { queryKey: chargeKeys.detail(vehicle, charging.charge?.id ?? '') },
      { queryKey: statsKeys.all(vehicle) },
    ])
  })

  it('reads nothing again while none is in progress', async () => {
    mocks.fetchChargeInProgress.mockResolvedValue(null)
    const queryClient = client()
    withSetup(() => useChargeInProgress(vehicle), { queryClient })
    await flushPromises()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await queryClient.refetchQueries({ queryKey: chargeKeys.inProgress(vehicle) })
    await flushPromises()
    expect(invalidate).not.toHaveBeenCalled()
  })
})

describe('useCharge', () => {
  // The ID of a charge in progress is no charge yet.
  it('asks nothing while not enabled', async () => {
    withSetup(() => useCharge(vehicle, 'x', false))
    await flushPromises()
    expect(mocks.fetchCharge).not.toHaveBeenCalled()
  })
})
