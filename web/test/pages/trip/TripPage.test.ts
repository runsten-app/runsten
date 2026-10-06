import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { TripPage } from '@/pages/trip'
import { ApiError } from '@/shared/api'
import trip from '@fixtures/trip.json'
import { mountWith, testRouter } from '@test/utils'

const { fetchTrip } = vi.hoisted(() => ({ fetchTrip: vi.fn() }))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  fetchTrip,
}))

beforeEach(() => {
  fetchTrip.mockReset()
})

async function page(id: string, query = '') {
  const router = testRouter()
  await router.push({
    name: 'trip',
    params: { vehicle: 'v1', id },
    query: { from: query || undefined },
  })
  const { wrapper } = mountWith(TripPage, { router, props: { vehicle: 'v1', id } })
  await flushPromises()
  return wrapper
}

describe('TripPage', () => {
  it('shows the trip, with a way back to the list as it was', async () => {
    fetchTrip.mockResolvedValue(trip)
    const w = await page(trip.id, '2026-09-28')
    expect(fetchTrip).toHaveBeenCalledWith('v1', trip.id)
    expect(w.find('.v-main h1').text()).toBe('Trip')
    expect(w.find('a.back').attributes('href')).toBe('/vehicles/v1/trips?from=2026-09-28')
    expect(w.find('.v-tab[aria-current="page"]').text()).toBe('Trips')
  })

  it('tells an unknown trip, with a link to the list', async () => {
    fetchTrip.mockRejectedValue(new ApiError(404, 'not_found', ''))
    const w = await page('nope')
    expect(w.find('.v-alert').text()).toBe('This trip does not exist.')
    expect(w.find('a.back').attributes('href')).toBe('/vehicles/v1/trips')
    expect(w.find('a.back').text()).toBe('Trips')
  })
})
