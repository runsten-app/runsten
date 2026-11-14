import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Trip } from '@/entities/trip'
import { sessionKeys } from '@/entities/session'
import { i18n } from '@/shared/i18n'
import { TripDetails } from '@/widgets/trip-details'
import session from '@fixtures/session.json'
import settings from '@fixtures/settings.json'
import unset from '@fixtures/settings-unset.json'
import stats from '@fixtures/stats.json'
import costed from '@fixtures/trip-cost.json'
import vehicle from '@fixtures/vehicle-model.json'
import { heard, mountWith, seen, testQueryClient } from '@test/utils'

// The public build's: no page lifts the limits.
vi.mock('@/entities/session/extensions', () => ({ limitsPage: null }))

const { fetchTrip, fetchStats, fetchVehicle, fetchSettings } = vi.hoisted(() => ({
  fetchTrip: vi.fn(),
  fetchStats: vi.fn(),
  fetchVehicle: vi.fn(),
  fetchSettings: vi.fn(),
}))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  fetchTrip,
}))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicle,
}))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  fetchSettings,
}))

beforeEach(() => {
  fetchTrip.mockReset()
  fetchStats.mockReset()
  fetchStats.mockResolvedValue(stats)
  fetchVehicle.mockReset()
  fetchVehicle.mockResolvedValue(vehicle)
  fetchSettings.mockReset()
  fetchSettings.mockResolvedValue(settings)
})

async function mounted(trip: Trip, unavailable: string[] = []) {
  fetchTrip.mockResolvedValue(trip)
  const queryClient = testQueryClient()
  queryClient.setQueryData(sessionKeys.current(), {
    ...session,
    limits: unavailable.length ? { history_from: null, unavailable, unread_vehicles: [] } : null,
  })
  const { wrapper } = mountWith(TripDetails, { queryClient, props: { vehicle: 'v1', id: 'x' } })
  await flushPromises()
  return wrapper.find('.trip-cost')
}

type Card = Awaited<ReturnType<typeof mounted>>

const notes = (c: Card) => c.findAll('.note').map((n) => seen(n.element))

describe('TripCost', () => {
  it('shows the amount, its price per kWh, the charges it rests on and where they charged', async () => {
    const c = await mounted(costed as Trip)
    expect(c.find('h2').text()).toBe('Cost')
    expect(seen(c.find('.amount').element)).toBe('€2.90 €9.06/100 km')
    expect(heard(c.find('.per-100km').element)).toBe('€9.06 per 100 km')
    expect(seen(c.find('.price .value').element)).toBe('€0.48295/kWh')
    expect(seen(c.find('.basis .value').element)).toBe('1 charge in the 30 days before')
    expect(c.find('.bar').attributes('aria-hidden')).toBe('true')
    expect(c.findAll('.bar span')).toHaveLength(1)
    expect(c.find('.legend').attributes('aria-label')).toBe('Where that energy was charged')
    expect(c.findAll('.legend li').map((l) => seen(l.element))).toEqual([
      'AC elsewhere 100% · €0.48295/kWh',
    ])
    expect(notes(c)).toEqual([i18n.global.t('trip.cost.note')])
  })

  it('splits the energy by place, then on DC elsewhere, and counts the charges left out', async () => {
    const trip = structuredClone(costed) as Trip
    if (!trip.cost) throw new Error('fixture without a cost')
    trip.cost = {
      ...trip.cost,
      min_minor: 300,
      max_minor: 350,
      price: {
        ...trip.cost.price,
        price_per_kwh: { min: 0.144, max: 0.16 },
        energy_kwh: 50,
        charges: 6,
        left_out: 1,
        shares: [
          {
            place: { id: 'p1', name: 'Home' },
            type: null,
            energy_kwh: 39,
            price_per_kwh: { min: 0.13, max: 0.13 },
          },
          { place: null, type: 'DC', energy_kwh: 11, price_per_kwh: { min: 0.49, max: 0.49 } },
        ],
      },
    }
    const c = await mounted(trip)
    expect(seen(c.find('.amount .figure').element)).toBe('€3.00 – €3.50')
    expect(seen(c.find('.price .value').element)).toBe('€0.144/kWh – €0.16/kWh')
    expect(heard(c.find('.price .value').element)).toBe('from €0.144/kWh to €0.16/kWh')
    expect(seen(c.find('.basis .value').element)).toBe('6 charges in the 30 days before')
    expect(c.findAll('.legend li').map((l) => seen(l.element))).toEqual([
      'Home 78% · €0.13/kWh',
      'DC elsewhere 22% · €0.49/kWh',
    ])
    expect(notes(c)).toEqual([
      i18n.global.t('trip.cost.note'),
      i18n.global.t('trip.cost.rangeNote'),
      '1 charge without a cost or an energy is not counted.',
    ])
  })

  it('says why a cost is unknown', async () => {
    let c = await mounted({ ...(costed as Trip), cost: null })
    expect(seen(c.find('.amount').element)).toBe('Unknown')
    expect(notes(c)).toEqual([i18n.global.t('trip.cost.noPrice')])
    c = await mounted({ ...(costed as Trip), cost: null, energy_kwh: null })
    expect(notes(c)).toEqual([i18n.global.t('trip.cost.noEnergy')])
  })

  it("tells a plug-in hybrid's cost is its electricity's", async () => {
    fetchVehicle.mockResolvedValue({
      ...vehicle,
      model: { ...vehicle.model, powertrain: 'plugin_hybrid' },
    })
    const c = await mounted({
      ...(costed as Trip),
      cost: { ...costed.cost, per_100km: null },
    } as Trip)
    expect(c.find('.per-100km').exists()).toBe(false)
    expect(notes(c)).toContain(i18n.global.t('trip.cost.hybrid'))
  })

  it('leads to the settings without a currency', async () => {
    fetchSettings.mockResolvedValue(unset)
    const c = await mounted({ ...(costed as Trip), cost: null })
    expect(c.find('.amount').exists()).toBe(false)
    expect(c.find('.choose-currency').attributes('href')).toBe('/settings')
  })

  it("says the costs are part of another offer, where the account's leaves them out", async () => {
    const c = await mounted({ ...(costed as Trip), cost: null }, ['costs'])
    expect(c.find('.feature-unavailable').text()).toContain(i18n.global.t('limits.costs'))
    expect(c.find('.amount').exists()).toBe(false)
  })
})
