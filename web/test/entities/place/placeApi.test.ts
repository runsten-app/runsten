import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  createPlace,
  deletePlace,
  fetchPlace,
  fetchPlaces,
  placeKeys,
  replacePlace,
  type PlaceFields,
} from '@/entities/place'
import place from '@fixtures/place.json'
import places from '@fixtures/places.json'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const { id, ...fields } = place
const path = `/places/${id}`

describe('place API', () => {
  it('lists the places and reads one', async () => {
    stubFetch({ 'GET /places': () => json(places), [`GET ${path}`]: () => json(place) })
    expect(await fetchPlaces()).toEqual(places.items)
    expect(await fetchPlace(id)).toEqual(place)
  })

  it('creates a place and replaces one, with their fields', async () => {
    const requests = stubFetch({
      'POST /places': () => json(place, 201),
      [`PUT ${path}`]: () => json(place),
    })
    expect(await createPlace(fields as PlaceFields)).toEqual(place)
    expect(await replacePlace(id, fields as PlaceFields)).toEqual(place)
    for (const r of requests) expect(await r.json()).toEqual(fields)
  })

  it('deletes a place', async () => {
    const requests = stubFetch({ [`DELETE ${path}`]: () => new Response(null, { status: 204 }) })
    await expect(deletePlace(id)).resolves.toBeUndefined()
    expect(requests).toHaveLength(1)
  })

  it('fails with the code of the contract', async () => {
    stubFetch({ 'POST /places': () => apiError(409, 'without_position_taken') })
    await expect(createPlace(fields as PlaceFields)).rejects.toMatchObject({
      status: 409,
      code: 'without_position_taken',
    })
  })

  it('has one key per query', () => {
    expect(placeKeys.list()).toEqual(['places'])
    expect(placeKeys.detail(id)).toEqual(['place', id])
  })
})
