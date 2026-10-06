import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { chargeKeys } from '@/entities/charge'
import { useCharge, useCharges } from '@/features/browse-charges'
import charge from '@fixtures/charge.json'
import charges from '@fixtures/charges.json'
import { QueryClient } from '@tanstack/vue-query'
import { withSetup } from '@test/utils'

const mocks = vi.hoisted(() => ({ fetchCharges: vi.fn(), fetchCharge: vi.fn() }))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  ...mocks,
}))

// As the app's: data is fresh for 30 s.
const client = () =>
  new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 } } })

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
})

const vehicle = 'v1'

describe('useCharges', () => {
  it('reads the only page, and has no more', async () => {
    mocks.fetchCharges.mockResolvedValue(charges)
    const { result: r } = withSetup(() => useCharges(vehicle, undefined, undefined))
    await flushPromises()
    expect(mocks.fetchCharges).toHaveBeenCalledWith(vehicle, {
      from: undefined,
      to: undefined,
      cursor: undefined,
    })
    expect(r.charges.value).toEqual(charges.items)
    expect(r.hasMore.value).toBe(false)
  })
})

describe('useCharge', () => {
  it('shows a charge of a list at once, without a call', async () => {
    const queryClient = client()
    queryClient.setQueryData(chargeKeys.list(vehicle), {
      pages: [charges],
      pageParams: [undefined],
    })
    const { result } = withSetup(() => useCharge(vehicle, charge.id), { queryClient })
    expect(result.charge.value).toEqual(charges.items[0])
    await flushPromises()
    expect(mocks.fetchCharge).not.toHaveBeenCalled()
  })

  it('reads a charge no list holds', async () => {
    mocks.fetchCharge.mockResolvedValue(charge)
    const { result } = withSetup(() => useCharge(vehicle, charge.id))
    await flushPromises()
    expect(mocks.fetchCharge).toHaveBeenCalledWith(vehicle, charge.id)
    expect(result.charge.value).toEqual(charge)
  })
})
