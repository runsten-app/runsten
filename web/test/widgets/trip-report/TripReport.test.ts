import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/shared/api'
import { TripReport } from '@/widgets/trip-report'
import costed from '@fixtures/trip-cost.json'
import page1 from '@fixtures/trips-page-1.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, seen } from '@test/utils'

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

const car = vehicles.items[0]
beforeEach(() => {
  fetchTrips.mockReset()
  fetchVehicle.mockReset()
  fetchVehicle.mockResolvedValue(car)
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mounted(props: Record<string, unknown> = {}) {
  const { wrapper } = mountWith(TripReport, { props: { vehicle: car?.id, ...props } })
  await flushPromises()
  return wrapper
}

describe('TripReport', () => {
  it('lists every trip of the period, oldest first, a page after another, with the totals', async () => {
    // Newest first, as the API gives them: the evening and the afternoon, then the morning.
    fetchTrips
      .mockResolvedValueOnce({ items: page1.items, next_cursor: 'next' })
      .mockResolvedValueOnce({ items: [costed], next_cursor: null })
    const w = await mounted({
      from: '2026-09-01T00:00:00Z',
      to: '2026-10-01T00:00:00Z',
      purpose: 'business',
    })
    expect(fetchTrips.mock.calls).toEqual([
      [
        car?.id,
        {
          from: '2026-09-01T00:00:00Z',
          to: '2026-10-01T00:00:00Z',
          purpose: 'business',
          limit: 200,
          cursor: undefined,
        },
      ],
      [
        car?.id,
        {
          from: '2026-09-01T00:00:00Z',
          to: '2026-10-01T00:00:00Z',
          purpose: 'business',
          limit: 200,
          cursor: 'next',
        },
      ],
    ])
    expect(w.find('h1').text()).toBe('Business trips')
    expect(w.find('.vehicle').text()).toContain(car?.vin)
    const rows = w
      .findAll('tbody tr')
      .map((r) => r.findAll('td').map((c) => plain(seen(c.element))))
    expect(rows.map((r) => r.slice(1))).toEqual([
      ['Maison de Tante Agathe', 'Elsewhere', '12,400 km → 12,432 km', '32 km', '€2.90'],
      ['Elsewhere', 'Elsewhere', '12,432 km → 12,440 km', '8 km', 'Unknown'],
      ['Elsewhere', 'Maison de Tante Agathe', '12,440 km → 12,473 km', '33 km', 'Unknown'],
    ])
    expect(w.findAll('tfoot th, tfoot td').map((c) => plain(seen(c.element)))).toEqual([
      '3 trips',
      '73 km',
      '€2.90',
    ])
    expect(w.text()).toContain('2 trips have no known cost: they are left out of the total.')
  })

  it('names the whole history and every purpose without a filter, and says when empty', async () => {
    fetchTrips.mockResolvedValue({ items: [], next_cursor: null })
    const w = await mounted()
    expect(w.find('h1').text()).toBe('Trips')
    expect(w.text()).toContain('Whole history')
    expect(w.find('.empty').text()).toBe('No trip in this period.')
    const none = await mounted({ purpose: 'none', from: '2026-09-01T00:00:00Z' })
    expect(none.find('h1').text()).toBe('Unmarked trips')
  })

  it('tells a failure', async () => {
    fetchTrips.mockRejectedValue(new ApiError(0, 'network', ''))
    const w = await mounted()
    expect(w.find('.v-alert').text()).toBe('The trips could not be loaded. Try again in a moment.')
  })
})
