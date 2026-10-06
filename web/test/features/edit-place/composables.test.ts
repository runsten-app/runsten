import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { chargeKeys } from '@/entities/charge'
import { placeKeys, type Place } from '@/entities/place'
import { statsKeys } from '@/entities/stats'
import { useDeletePlace, usePlace, usePlaces, useSavePlace } from '@/features/edit-place'
import place from '@fixtures/place.json'
import places from '@fixtures/places.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchPlaces: vi.fn(),
  fetchPlace: vi.fn(),
  createPlace: vi.fn(),
  replacePlace: vi.fn(),
  deletePlace: vi.fn(),
}))
vi.mock('@/entities/place', async (original) => ({
  ...(await original<typeof import('@/entities/place')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

const { id, ...fields } = place as Place
// What a change of a place makes stale: the places, and the costs of the charges.
const stale = [
  placeKeys.list(),
  placeKeys.unpricedAll(),
  chargeKeys.all(),
  chargeKeys.details(),
  statsKeys.every(),
]

describe('usePlaces and usePlace', () => {
  it('read the places, and one', async () => {
    api.fetchPlaces.mockResolvedValue(places.items)
    api.fetchPlace.mockResolvedValue(place)
    const { result } = withSetup(() => ({ list: usePlaces(), one: usePlace(id) }))
    await flushPromises()
    expect(result.list.places.value).toEqual(places.items)
    expect(result.one.place.value).toEqual(place)
    expect(api.fetchPlace).toHaveBeenCalledWith(id)
  })

  it('ask nothing for a new place, then read it once it has an ID', async () => {
    api.fetchPlace.mockResolvedValue(place)
    const current = ref<string>()
    const { result } = withSetup(() => usePlace(current))
    await flushPromises()
    expect(api.fetchPlace).not.toHaveBeenCalled()
    expect(result.place.value).toBeUndefined()
    current.value = id
    await flushPromises()
    expect(result.place.value).toEqual(place)
  })
})

describe('useSavePlace', () => {
  it('creates a place without an ID, caches it and refreshes what it changes', async () => {
    api.createPlace.mockResolvedValue(place)
    const { result, queryClient } = withSetup(useSavePlace)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(await result.savePlace({ fields })).toEqual(place)
    expect(api.createPlace).toHaveBeenCalledWith(fields)
    expect(api.replacePlace).not.toHaveBeenCalled()
    expect(queryClient.getQueryData(placeKeys.detail(id))).toEqual(place)
    expect(invalidate.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)).toEqual(
      stale,
    )
  })

  it('replaces a place with an ID', async () => {
    api.replacePlace.mockResolvedValue(place)
    const { result } = withSetup(useSavePlace)
    await result.savePlace({ id, fields })
    expect(api.replacePlace).toHaveBeenCalledWith(id, fields)
    expect(api.createPlace).not.toHaveBeenCalled()
  })
})

describe('useDeletePlace', () => {
  it('deletes a place, forgets it and refreshes what it changes', async () => {
    api.deletePlace.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(useDeletePlace)
    queryClient.setQueryData(placeKeys.detail(id), place)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.deletePlace(id)
    expect(api.deletePlace).toHaveBeenCalledWith(id)
    expect(queryClient.getQueryData(placeKeys.detail(id))).toBeUndefined()
    expect(invalidate.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)).toEqual(
      stale,
    )
  })
})
