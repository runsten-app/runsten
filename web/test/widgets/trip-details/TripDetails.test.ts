import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import type { Stats } from '@/entities/stats'
import { TripDetails } from '@/widgets/trip-details'
import stats from '@fixtures/stats.json'
import trip from '@fixtures/trip.json'
import page1 from '@fixtures/trips-page-1.json'
import { withMapTiles, withoutMapTiles } from '@test/mapMeta'
import { mountWith } from '@test/utils'

const { fetchTrip, fetchStats } = vi.hoisted(() => ({ fetchTrip: vi.fn(), fetchStats: vi.fn() }))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  fetchTrip,
}))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats,
}))

// month is the statistics of the trip's month: 16 kWh/100 km on average, 20 for the
// trips of 20 to 50 km.
function month(): Stats {
  const s = structuredClone(stats) as Stats
  s.totals.trips.consumption_kwh_per_100km = 16
  const band = s.trips_by_distance.bands[2]
  if (band) band.consumption_kwh_per_100km = 20
  return s
}

beforeEach(() => {
  fetchTrip.mockReset()
  fetchStats.mockReset()
  fetchStats.mockResolvedValue(month())
})
afterEach(() => {
  i18n.global.locale.value = 'en'
  withoutMapTiles()
})

const plain = (s: string) => s.replace(/\s+/g, ' ').trim()

async function mounted(body: unknown) {
  if (body instanceof Error) fetchTrip.mockRejectedValue(body)
  else fetchTrip.mockResolvedValue(body)
  const { wrapper } = mountWith(TripDetails, { props: { vehicle: 'v1', id: 'x' } })
  await flushPromises()
  return wrapper
}

type Wrapper = Awaited<ReturnType<typeof mounted>>

// facts are the labels and values of a card.
function facts(w: Wrapper, card: string) {
  return Object.fromEntries(
    w.findAll(`.${card} .fact`).map((f) => [f.find('dt').text(), plain(f.find('.value').text())]),
  )
}

describe('TripDetails', () => {
  it('shows the bounds, the honest duration and the figures', async () => {
    const w = await mounted(trip)
    expect(w.find('h1').text()).toBe('Trip')
    expect(w.find('header').text()).toContain('Monday, 28 September')
    expect(facts(w, 'when')).toEqual({
      Start: '06:55–07:01',
      End: '07:39–07:40',
      Duration: '38–45 mins',
    })
    expect(facts(w, 'figures')).toEqual({
      Distance: '32 km',
      'Energy (estimated)': '6 kWh',
      'State of charge': '62% → 55%',
      Range: '248 km → 218 km',
      Odometer: '12,400 km → 12,432 km',
    })
    expect(w.find('.figures .note').text()).toContain('No meter is read.')
    expect(w.find('.reconstructed-note').exists()).toBe(false)
  })

  it('names the place of a position, else gives its coordinates, with a link to OpenStreetMap', async () => {
    const w = await mounted(trip)
    expect(facts(w, 'positions')).toEqual({
      Start: 'Maison de Tante Agathe',
      End: '45.77970, 4.92700',
    })
    // The place's name keeps the coordinates under it.
    expect(w.find('.positions .coordinates').text()).toBe('45.76400, 4.83570')
    expect(w.find('.positions .place-link').attributes('href')).toBe(
      '/settings/places/5e1f0000-0000-4000-8000-000000000001',
    )
    // A position outside every place can become one.
    expect(w.find('.positions .create-place').attributes('href')).toBe(
      '/settings/places/new?lat=45.77970&lon=4.92700',
    )
    const osm = w.findAll('.positions a[target="_blank"]')
    expect(osm.map((a) => a.attributes('href'))).toEqual([
      'https://www.openstreetmap.org/?mlat=45.76400&mlon=4.83570#map=16/45.76400/4.83570',
      'https://www.openstreetmap.org/?mlat=45.77970&mlon=4.92700#map=16/45.77970/4.92700',
    ])
    expect(osm[0]?.attributes('rel')).toBe('noreferrer')
    expect(w.find('img').exists()).toBe(false)
  })

  it('names a position outside the places by its address, its coordinates under it', async () => {
    const w = await mounted({ ...trip, end_address: 'Avenue Roger Salengro, Villeurbanne' })
    expect(facts(w, 'positions')).toEqual({
      Start: 'Maison de Tante Agathe',
      End: 'Avenue Roger Salengro, Villeurbanne',
    })
    expect(w.findAll('.positions .coordinates').map((c) => c.text())).toEqual([
      '45.76400, 4.83570',
      '45.77970, 4.92700',
    ])
    // Still no place there: one can be created.
    expect(w.find('.positions .create-place').exists()).toBe(true)
  })

  it('leaves out the links to OpenStreetMap where a map shows the positions', async () => {
    withMapTiles()
    const w = await mounted(trip)
    expect(w.find('.positions .map-view').exists()).toBe(true)
    expect(w.findAll('.positions a[target="_blank"]')).toHaveLength(0)
    expect(w.find('.positions .create-place').exists()).toBe(true)
    w.unmount()
  })

  it("keeps the vehicle's own figures apart, for comparison", async () => {
    const w = await mounted(trip)
    const card = w.find('.vehicle-reported')
    expect(card.find('.v-card-title').text()).toBe('Reported by the vehicle (for comparison)')
    expect(facts(w, 'vehicle-reported')).toEqual({
      'Trip meter': '32.2 km',
      Consumption: '18.1 kWh/100 km',
    })
    expect(card.find('.note').text()).toContain('unknown')
  })

  it("sets the trip's consumption beside its month's, and that of trips of its length", async () => {
    const w = await mounted(trip)
    // 6 kWh over 32 km, started on 28 September: the month's statistics, in the reader's
    // time zone (UTC here), totals only.
    expect(fetchStats).toHaveBeenCalledWith('v1', {
      from: '2026-09-01T00:00:00.000Z',
      to: '2026-10-01T00:00:00.000Z',
      tz: 'UTC',
      bucket: undefined,
    })
    expect(facts(w, 'consumption')).toEqual({
      'This trip (estimated)': '18.8 kWh/100 km',
      'Average of September': '16 kWh/100 km',
      'Trips 20 to 50 km in September': '20 kWh/100 km',
    })
    expect(w.findAll('.consumption .gap').map((g) => plain(g.text()))).toEqual([
      '+17% for this trip',
      '-6% for this trip',
    ])
    const link = w.find('.consumption .stats-link')
    expect(plain(link.text())).toBe('See the statistics of September')
    expect(link.attributes('href')).toBe('/vehicles/v1/stats?from=2026-09-01&to=2026-09-30')
  })

  it('compares a reconstructed trip with the month only: it may hold several', async () => {
    const w = await mounted({ ...trip, reconstructed: true, energy_kwh: null })
    expect(facts(w, 'consumption')).toEqual({
      'This trip (estimated)': 'Unknown',
      'Average of September': '16 kWh/100 km',
    })
    expect(w.find('.consumption .gap').exists()).toBe(false)
  })

  it('tells a reconstructed trip: one interval, no duration, unknowns', async () => {
    const w = await mounted(page1.items[1])
    const note = w.find('.reconstructed-note')
    expect(note.find('.event-when').text()).toBe('Happened between 12:00 and 13:00')
    expect(note.find('.reconstructed').exists()).toBe(true)
    expect(note.text()).toContain('revealed by the odometer or the state of charge')
    expect(w.find('.when').exists()).toBe(false)
    expect(w.text()).not.toContain('Duration')
    expect(facts(w, 'figures')).toMatchObject({
      Distance: '8 km',
      'Energy (estimated)': 'Unknown',
      'State of charge': 'Unknown',
    })
    expect(facts(w, 'positions')).toEqual({ Start: 'Unknown', End: 'Unknown' })
    expect(w.findAll('.positions a')).toHaveLength(0)
    expect(facts(w, 'vehicle-reported')).toEqual({
      'Trip meter': 'Unknown',
      Consumption: 'Unknown',
    })
  })

  it('speaks the chosen language', async () => {
    i18n.global.locale.value = 'fr'
    const w = await mounted(trip)
    expect(w.find('header').text()).toContain('lundi 28 septembre 2026')
    expect(facts(w, 'when')).toEqual({
      Début: '06:55 – 07:01',
      Fin: '07:39 – 07:40',
      Durée: '38–45 min',
    })
  })

  it.each([
    [new ApiError(404, 'not_found', ''), 'This trip does not exist.'],
    [new ApiError(500, 'internal', ''), 'The trip could not be loaded. Try again in a moment.'],
  ])('tells a failure (%s)', async (e, message) => {
    const w = await mounted(e)
    expect(w.find('.v-alert').text()).toBe(message)
    expect(w.find('article').exists()).toBe(false)
  })

  // Each card is a section of the page, under its <h1>.
  it('titles its cards with headings', async () => {
    const w = await mounted(trip)
    expect(w.findAll('.v-card-title').map((h) => [h.element.tagName, h.text()])).toEqual([
      ['H2', 'When'],
      ['H2', 'Figures'],
      ['H2', 'Consumption'],
      ['H2', 'Positions'],
      ['H2', 'Reported by the vehicle (for comparison)'],
    ])
  })
})
