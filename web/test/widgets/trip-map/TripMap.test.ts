import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TripMap as Map } from '@/entities/trip'
import { i18n } from '@/shared/i18n'
import { TripMap } from '@/widgets/trip-map'
import fixture from '@fixtures/trip-map.json'
import { mountWith } from '@test/utils'

const { fetchTripMap } = vi.hoisted(() => ({ fetchTripMap: vi.fn() }))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  fetchTripMap,
}))

beforeEach(() => {
  fetchTripMap.mockReset()
  i18n.global.locale.value = 'en'
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mount(map: Map, props: Record<string, unknown> = {}) {
  fetchTripMap.mockResolvedValue(map)
  const { wrapper } = mountWith(TripMap, { props: { vehicle: 'v1', ...props } })
  await flushPromises()
  return wrapper
}

describe('TripMap', () => {
  it('sums the map and lists the journeys made again, named by place or address', async () => {
    const w = await mount(fixture as Map, { from: '2026-09-01T00:00:00Z' })
    expect(fetchTripMap).toHaveBeenCalledWith('v1', { from: '2026-09-01T00:00:00Z', to: undefined })
    expect(plain(w.find('.trip-map p').text())).toBe('2 trips · 2 spots · 1 link')
    expect(w.text()).toContain('One trip without a position is left out.')
    const items = w.findAll('.frequent li').map((li) => plain(li.text()))
    expect(items).toEqual(['Maison de Tante Agathe ↔ 45.77970, 4.92700 2 trips · 65 km'])

    const named = structuredClone(fixture) as Map
    named.spots[1] = { ...fixture.spots[1], address: 'Rue Garibaldi, Lyon' } as Map['spots'][number]
    const again = await mount(named)
    expect(again.find('.frequent li').text()).toContain(
      'Maison de Tante Agathe ↔ Rue Garibaldi, Lyon',
    )
  })

  it('tells the trips of a journey whose distance is unknown', async () => {
    const partly = structuredClone(fixture) as Map
    partly.links = [{ ...fixture.links[0], distance_unknown: 1 } as Map['links'][number]]
    const li = (await mount(partly)).find('.frequent li')
    expect(plain(li.text())).toMatch(/2 trips · 65 km · distance unknown for 1 trip$/)
  })

  it('lists no journey driven once, and shows nothing without a trip', async () => {
    const once = structuredClone(fixture) as Map
    once.links = [{ ...fixture.links[0], trips: 1 } as Map['links'][number]]
    expect((await mount(once)).find('.frequent').exists()).toBe(false)

    const none: Map = {
      ...(fixture as Map),
      trips: 0,
      trips_without_position: 0,
      spots: [],
      links: [],
    }
    expect((await mount(none)).find('.trip-map').exists()).toBe(false)
  })

  it('tells the trips without a position when none can be drawn', async () => {
    const lost: Map = {
      ...(fixture as Map),
      trips: 0,
      trips_without_position: 2,
      spots: [],
      links: [],
    }
    const w = await mount(lost)
    expect(w.find('.map-view').exists()).toBe(false)
    expect(w.find('.trip-map').text()).toContain('2 trips without a position are left out.')
  })

  it('folds every spot with its departures and arrivals, and every link, those driven once too', async () => {
    const once = structuredClone(fixture) as Map
    once.links = [{ ...fixture.links[0], trips: 1 } as Map['links'][number]]
    // A spot reached by a round trip only: on the map without a line, in the list still.
    once.spots.push({
      ...fixture.spots[1],
      position: { lat: 45.5, lon: 4.9 },
      departures: 1,
      arrivals: 1,
    } as Map['spots'][number])
    const every = (await mount(once)).find('.trip-map details.every')
    expect(plain(every.find('summary').text())).toBe('Every spot and journey of the map')
    expect(every.findAll('ol.spots li').map((li) => plain(li.text()))).toEqual([
      'Maison de Tante Agathe: 1 departure, 1 arrival',
      '45.77970, 4.92700: 1 departure, 1 arrival',
      '45.50000, 4.90000: 1 departure, 1 arrival',
    ])
    expect(every.findAll('ol.journeys li').map((li) => plain(li.text()))).toEqual([
      'Maison de Tante Agathe ↔ 45.77970, 4.92700: 1 trip, 65 km',
    ])
  })

  it('says when the map could not be read', async () => {
    fetchTripMap.mockRejectedValue(new Error('down'))
    const { wrapper } = mountWith(TripMap, { props: { vehicle: 'v1' } })
    await flushPromises()
    await vi.waitFor(() => expect(wrapper.find('.trip-map-error').exists()).toBe(true), {
      timeout: 5000,
    })
    expect(wrapper.text()).toContain('The map of the trips could not be loaded.')
  })
})
