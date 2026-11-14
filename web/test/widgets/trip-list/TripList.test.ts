import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import { TripList } from '@/widgets/trip-list'
import page1 from '@fixtures/trips-page-1.json'
import page2 from '@fixtures/trips-page-2.json'
import imported from '@fixtures/trip-imported.json'
import costed from '@fixtures/trip-cost.json'
import settings from '@fixtures/settings.json'
import unset from '@fixtures/settings-unset.json'
import { heard, mountWith, seen, testRouter } from '@test/utils'

const { fetchTrips, fetchSettings } = vi.hoisted(() => ({
  fetchTrips: vi.fn(),
  fetchSettings: vi.fn(),
}))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  fetchTrips,
}))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  fetchSettings,
}))

beforeEach(() => {
  fetchTrips.mockReset()
  fetchSettings.mockReset()
  fetchSettings.mockResolvedValue(settings)
})

const vehicle = 'v1'
const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mounted(props: Record<string, unknown> = {}, path = `/vehicles/${vehicle}/trips`) {
  const router = testRouter()
  await router.push(path)
  const { wrapper } = mountWith(TripList, { router, props: { vehicle, ...props } })
  await flushPromises()
  return wrapper
}

describe('TripList', () => {
  it("shows each trip's cost on its right, its cost per 100 km below, else unknown", async () => {
    const range = { ...costed.cost, min_minor: 310, max_minor: 345 }
    fetchTrips.mockResolvedValue({
      items: [costed, { ...costed, id: 'b', cost: range }, { ...costed, id: 'c', cost: null }],
      next_cursor: null,
    })
    const w = await mounted()
    const costs = w.findAll('.trip-cost')
    expect(costs.map((c) => seen(c.element))).toEqual([
      '€2.90 €9.06/100 km',
      '€3.10 – €3.45 €9.06/100 km',
      'Unknown cost',
    ])
    expect(heard(costs[1]?.element ?? document.body)).toBe('from €3.10 to €3.45 €9.06 per 100 km')
  })

  it('shows no cost without a currency', async () => {
    fetchSettings.mockResolvedValue(unset)
    fetchTrips.mockResolvedValue({ items: [costed], next_cursor: null })
    const w = await mounted()
    expect(w.find('.trip-cost').exists()).toBe(false)
  })

  it('shows the trips by day, each a link to its details', async () => {
    fetchTrips.mockResolvedValue(page2)
    const w = await mounted({}, `/vehicles/${vehicle}/trips?from=2026-09-28`)
    expect(w.find('h2').text()).toBe('Monday, 28 September 2026')
    const row = w.find('a.trip-summary')
    expect(row.find('.event-when').text()).toBe('06:55–07:01 → 07:40')
    expect(plain(row.find('.figures').text())).toBe('32 km · 38–45 mins · 62% → 55%')
    // It starts at a place: the route names it, and reads as words.
    expect(row.find('.route [aria-hidden]').text()).toBe('Maison de Tante Agathe → Elsewhere')
    expect(row.find('.route .d-sr-only').text()).toBe('From Maison de Tante Agathe to Elsewhere')
    // The details keep the period, for the way back.
    expect(row.attributes('href')).toBe(
      `/vehicles/${vehicle}/trips/2026-09-28T07:01:00.000000Z?from=2026-09-28`,
    )
    expect(w.find('.end').text()).toBe('No more trips.')
    expect(w.find('.load-more').exists()).toBe(false)
  })

  it('names an end outside the places by its address', async () => {
    fetchTrips.mockResolvedValue({
      ...page2,
      items: [{ ...page2.items[0], end_address: 'Avenue Roger Salengro, Villeurbanne' }],
    })
    const w = await mounted({}, `/vehicles/${vehicle}/trips?from=2026-09-28`)
    expect(w.find('.route [aria-hidden]').text()).toBe(
      'Maison de Tante Agathe → Avenue Roger Salengro, Villeurbanne',
    )
  })

  it('tells a reconstructed trip, without a duration', async () => {
    fetchTrips.mockResolvedValue(page1)
    const w = await mounted()
    const [seen, reconstructed] = w.findAll('.trip-summary')
    expect(seen?.find('.reconstructed').exists()).toBe(false)
    expect(reconstructed?.find('.event-when').text()).toBe('Happened between 12:00 and 13:00')
    // The badge explains itself to a screen reader, in words an instance may change.
    expect(plain(reconstructed?.find('.reconstructed').text())).toBe(
      `Reconstructed: ${i18n.global.t('event.reconstructedHelp')}`,
    )
    expect(reconstructed?.find('.figures').text()).toBe('8 km')
    // Without a position at a place, no route.
    expect(reconstructed?.find('.route').exists()).toBe(false)
    // The first trip has no state of charge: none is shown, rather than a guess.
    expect(plain(seen?.find('.figures').text())).toBe('33 km · 43–50 mins')
  })

  it('tells an imported trip by its app, with its exact times and duration', async () => {
    fetchTrips.mockResolvedValue({ items: [imported], next_cursor: null })
    const w = await mounted()
    const row = w.find('a.trip-summary')
    expect(row.find('.imported').text()).toBe('Imported from Aximote')
    expect(row.find('.reconstructed').exists()).toBe(false)
    expect(row.find('.event-when').text()).toBe('17:02 → 17:41')
    expect(plain(row.find('.figures').text())).toBe('32 km · 39 mins · 81% → 74%')
    expect(row.find('.route [aria-hidden]').text()).toBe(
      'Boulevard Vivier-Merle, Lyon → Rue de la République, Lyon',
    )
    expect(row.attributes('href')).toBe(`/vehicles/${vehicle}/trips/aximote:trip-8812`)
  })

  it('loads more until the last page', async () => {
    fetchTrips.mockResolvedValueOnce(page1).mockResolvedValueOnce(page2)
    const w = await mounted()
    expect(w.findAll('.trip-summary')).toHaveLength(2)
    expect(w.find('.end').exists()).toBe(false)
    await w.find('.load-more').trigger('click')
    await flushPromises()
    expect(fetchTrips).toHaveBeenLastCalledWith(vehicle, {
      from: undefined,
      to: undefined,
      cursor: page1.next_cursor,
    })
    expect(w.findAll('.trip-summary')).toHaveLength(3)
    // All three trips started on the same day.
    expect(w.findAll('h2')).toHaveLength(1)
    expect(w.find('.load-more').exists()).toBe(false)
    expect(w.find('.end').exists()).toBe(true)
  })

  it('keeps the trips when more fail to load', async () => {
    fetchTrips.mockResolvedValueOnce(page1).mockRejectedValueOnce(new ApiError(500, 'internal', ''))
    const w = await mounted()
    await w.find('.load-more').trigger('click')
    await flushPromises()
    expect(w.findAll('.trip-summary')).toHaveLength(2)
    expect(w.find('.v-alert').text()).toBe('More trips could not be loaded. Try again.')
    expect(w.find('.load-more').exists()).toBe(true)
  })

  it.each([
    [{}, 'No trip recorded yet.'],
    [{ from: '2026-09-01T00:00:00.000Z' }, 'No trip in this period.'],
  ])('tells an empty list (%j)', async (props, message) => {
    fetchTrips.mockResolvedValue({ items: [], next_cursor: null })
    const w = await mounted(props)
    expect(w.find('.empty').text()).toBe(message)
    expect(w.find('.end').exists()).toBe(false)
  })

  it.each([
    [new ApiError(400, 'invalid_parameter', 'from'), 'This period is not valid.'],
    [new ApiError(404, 'not_found', ''), 'This vehicle does not exist.'],
    [new ApiError(0, 'network', ''), 'The trips could not be loaded. Try again in a moment.'],
  ])('tells a failure (%s)', async (e, message) => {
    fetchTrips.mockRejectedValue(e)
    const w = await mounted()
    expect(w.find('.v-alert').text()).toBe(message)
  })

  // A list's items are listitems: its rows are links, the divider goes inside an item.
  it('holds listitems only, each with its link', async () => {
    fetchTrips.mockResolvedValue(page1)
    const w = await mounted()
    for (const list of w.findAll('[role="list"]')) {
      const items = list.element.children
      expect(items.length).toBeGreaterThan(0)
      for (const item of items) {
        expect(item.getAttribute('role')).toBe('listitem')
        expect(item.querySelector('a.trip-summary')).not.toBeNull()
      }
    }
  })
})
