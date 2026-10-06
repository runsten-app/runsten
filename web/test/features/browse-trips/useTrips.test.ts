import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { tripKeys } from '@/entities/trip'
import { useTrip, useTrips } from '@/features/browse-trips'
import trip from '@fixtures/trip.json'
import page1 from '@fixtures/trips-page-1.json'
import page2 from '@fixtures/trips-page-2.json'
import { QueryClient } from '@tanstack/vue-query'
import { withSetup } from '@test/utils'

const mocks = vi.hoisted(() => ({ fetchTrips: vi.fn(), fetchTrip: vi.fn() }))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  ...mocks,
}))

// As the app's: data is fresh for 30 s.
const client = () =>
  new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 } } })

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
})

const vehicle = 'v1'
const from = '2026-09-28T00:00:00.000Z'

describe('useTrips', () => {
  it('reads the first page, the next with its cursor, and stops at the last', async () => {
    mocks.fetchTrips.mockResolvedValueOnce(page1).mockResolvedValueOnce(page2)
    const { result: r } = withSetup(() => useTrips(vehicle, from, undefined))
    await flushPromises()
    expect(mocks.fetchTrips).toHaveBeenLastCalledWith(vehicle, {
      from,
      to: undefined,
      cursor: undefined,
    })
    expect(r.trips.value).toEqual(page1.items)
    expect(r.hasMore.value).toBe(true)

    await r.loadMore()
    await flushPromises()
    expect(mocks.fetchTrips).toHaveBeenLastCalledWith(vehicle, {
      from,
      to: undefined,
      cursor: page1.next_cursor,
    })
    expect(r.trips.value).toEqual([...page1.items, ...page2.items])
    expect(r.hasMore.value).toBe(false)
  })

  it('starts again from the first page when the period changes', async () => {
    mocks.fetchTrips.mockResolvedValue(page2)
    const to = ref<string | undefined>(undefined)
    const { queryClient } = withSetup(() => useTrips(vehicle, undefined, to))
    await flushPromises()
    to.value = '2026-09-29T00:00:00.000Z'
    await flushPromises()
    expect(mocks.fetchTrips).toHaveBeenCalledTimes(2)
    expect(mocks.fetchTrips).toHaveBeenLastCalledWith(vehicle, {
      from: undefined,
      to: to.value,
      cursor: undefined,
    })
    expect(queryClient.getQueryData(tripKeys.list(vehicle, undefined, to.value))).toBeDefined()
  })
})

describe('useTrip', () => {
  it('shows a trip of a list at once, without a call', async () => {
    const queryClient = client()
    queryClient.setQueryData(tripKeys.list(vehicle, from), {
      pages: [page1, page2],
      pageParams: [undefined, page1.next_cursor],
    })
    const { result } = withSetup(() => useTrip(vehicle, trip.id), { queryClient })
    expect(result.trip.value).toEqual(page2.items[0])
    await flushPromises()
    expect(mocks.fetchTrip).not.toHaveBeenCalled()
  })

  it('reads a trip no list holds', async () => {
    mocks.fetchTrip.mockResolvedValue(trip)
    const { result } = withSetup(() => useTrip(vehicle, trip.id))
    expect(result.trip.value).toBeUndefined()
    await flushPromises()
    expect(mocks.fetchTrip).toHaveBeenCalledWith(vehicle, trip.id)
    expect(result.trip.value).toEqual(trip)
  })

  it('reads it again once the list is stale', async () => {
    mocks.fetchTrip.mockResolvedValue(trip)
    const queryClient = client()
    queryClient.setQueryData(
      tripKeys.list(vehicle),
      { pages: [page2], pageParams: [undefined] },
      {
        updatedAt: Date.now() - 60 * 60_000,
      },
    )
    withSetup(() => useTrip(vehicle, trip.id), { queryClient })
    await flushPromises()
    expect(mocks.fetchTrip).toHaveBeenCalledOnce()
  })
})
