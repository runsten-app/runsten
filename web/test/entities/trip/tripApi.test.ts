import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchTrip, fetchTrips, setTripMarks, tripKeys, tripsCsvPath } from '@/entities/trip'
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

  it('lists the trips of a purpose', async () => {
    const requests = stubFetch({ [`GET /vehicles/${vehicle}/trips`]: () => json(page1) })
    await fetchTrips(vehicle, { purpose: 'none', limit: 200 })
    const q = new URL(requests[0]?.url ?? '').searchParams
    expect(Object.fromEntries(q)).toEqual({ purpose: 'none', limit: '200' })
    expect(tripsCsvPath(vehicle, { from: 'a', purpose: 'business' })).toBe(
      `api/v1/vehicles/${vehicle}/trips.csv?from=a&purpose=business`,
    )
  })

  it('marks trips with a purpose', async () => {
    const requests = stubFetch({
      [`PUT /vehicles/${vehicle}/trip-marks`]: () => new Response(null, { status: 204 }),
    })
    await setTripMarks(vehicle, [trip.id], 'commute')
    expect(await requests[0]?.json()).toEqual({ trips: [trip.id], purpose: 'commute' })
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
    expect(tripKeys.list(vehicle)).toEqual(['trips', vehicle, null, null, null])
    expect(tripKeys.list(vehicle, 'a', 'b')).toEqual(['trips', vehicle, 'a', 'b', null])
    expect(tripKeys.list(vehicle, 'a', 'b', 'business')).toEqual([
      'trips',
      vehicle,
      'a',
      'b',
      'business',
    ])
    expect(tripKeys.all()).toEqual(tripKeys.lists(vehicle).slice(0, 1))
    expect(tripKeys.list(vehicle).slice(0, 2)).toEqual(tripKeys.lists(vehicle))
    expect(tripKeys.detail(vehicle, trip.id)).toEqual(['trip', vehicle, trip.id])
  })
})
