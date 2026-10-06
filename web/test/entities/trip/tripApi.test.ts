import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchTrip, fetchTrips, tripKeys } from '@/entities/trip'
import trip from '@fixtures/trip.json'
import page1 from '@fixtures/trips-page-1.json'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const vehicle = 'v1'

describe('trip API', () => {
  it('lists a page of trips, with only the parameters given', async () => {
    const requests = stubFetch({ [`GET /vehicles/${vehicle}/trips`]: () => json(page1) })
    expect(await fetchTrips(vehicle)).toEqual(page1)
    expect(new URL(requests[0]?.url ?? '').search).toBe('')
    await fetchTrips(vehicle, { from: '2026-09-28T00:00:00.000Z', cursor: 'abc' })
    const q = new URL(requests[1]?.url ?? '').searchParams
    expect(Object.fromEntries(q)).toEqual({ from: '2026-09-28T00:00:00.000Z', cursor: 'abc' })
  })

  it('reads a trip, its ID encoded in the path', async () => {
    const requests = stubFetch({
      [`GET /vehicles/${vehicle}/trips/${encodeURIComponent(trip.id)}`]: () => json(trip),
    })
    expect(await fetchTrip(vehicle, trip.id)).toEqual(trip)
    expect(requests[0]?.url).toContain(`/trips/${encodeURIComponent(trip.id)}`)
    expect(requests[0]?.url).toContain('%3A')
  })

  it('fails with the code of the contract', async () => {
    stubFetch({ [`GET /vehicles/${vehicle}/trips/nope`]: () => apiError(404, 'not_found') })
    await expect(fetchTrip(vehicle, 'nope')).rejects.toMatchObject({ code: 'not_found' })
  })

  it('has one key per query, the lists under one prefix', () => {
    expect(tripKeys.list(vehicle)).toEqual(['trips', vehicle, null, null])
    expect(tripKeys.list(vehicle, 'a', 'b')).toEqual(['trips', vehicle, 'a', 'b'])
    expect(tripKeys.list(vehicle).slice(0, 2)).toEqual(tripKeys.lists(vehicle))
    expect(tripKeys.detail(vehicle, trip.id)).toEqual(['trip', vehicle, trip.id])
  })
})
