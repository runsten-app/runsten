import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { TripReportPage } from '@/pages/trip-report'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

const { fetchTrips, fetchVehicle } = vi.hoisted(() => ({
  fetchTrips: vi.fn(),
  fetchVehicle: vi.fn(),
}))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  fetchTrips,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicle,
}))

beforeEach(() => {
  fetchTrips.mockReset()
  fetchTrips.mockResolvedValue({ items: [], next_cursor: null })
  fetchVehicle.mockResolvedValue(vehicles.items[0])
})

describe('TripReportPage', () => {
  it('reads the period and purpose of the URL, leads back to the list, and prints', async () => {
    const router = testRouter()
    await router.push('/vehicles/v1/trips/report?from=2026-09-01&purpose=commute&place=x')
    const { wrapper } = mountWith(TripReportPage, { router, props: { vehicle: 'v1' } })
    await flushPromises()
    expect(fetchTrips).toHaveBeenCalledWith(
      'v1',
      expect.objectContaining({ from: '2026-09-01T00:00:00.000Z', purpose: 'commute' }),
    )
    expect(wrapper.find('.back').attributes('href')).toBe(
      '/vehicles/v1/trips?from=2026-09-01&purpose=commute',
    )
    const print = vi.spyOn(window, 'print').mockImplementation(() => {})
    await wrapper.find('.print').trigger('click')
    expect(print).toHaveBeenCalled()
  })

  it('tells a period that is not one, and asks nothing', async () => {
    const router = testRouter()
    await router.push('/vehicles/v1/trips/report?from=2026-09-30&to=2026-09-01')
    const { wrapper } = mountWith(TripReportPage, { router, props: { vehicle: 'v1' } })
    await flushPromises()
    expect(wrapper.find('.invalid-period').text()).toBe('This period is not valid.')
    expect(fetchTrips).not.toHaveBeenCalled()
  })
})
