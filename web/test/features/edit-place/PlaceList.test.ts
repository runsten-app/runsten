import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PlaceList } from '@/features/edit-place'
import { ApiError } from '@/shared/api'
import place from '@fixtures/place.json'
import places from '@fixtures/places.json'
import settings from '@fixtures/settings.json'
import { mountWith } from '@test/utils'

const { fetchPlaces } = vi.hoisted(() => ({ fetchPlaces: vi.fn() }))
vi.mock('@/entities/place', async (original) => ({
  ...(await original<typeof import('@/entities/place')>()),
  fetchPlaces,
}))

const plain = (s: string) => s.replace(/\s/g, ' ')
const eur = { code: 'EUR', minor_digits: 2 }
const to = (place: string) => ({ name: 'place', params: { place } })

beforeEach(() => {
  fetchPlaces.mockReset()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-27T10:00:00Z'))
})
afterEach(() => {
  vi.useRealTimers()
})

async function list(currency: typeof eur | null = eur, maxPlaces = settings.limits.places) {
  const m = mountWith(PlaceList, { props: { currency, maxPlaces, to } })
  await flushPromises()
  return m.wrapper
}

describe('PlaceList', () => {
  it('shows each place with its radius and the price of today, and links to it', async () => {
    fetchPlaces.mockResolvedValue(places.items)
    const w = await list()
    const rows = w.findAll('[role="listitem"]')
    expect(rows).toHaveLength(2)
    expect(rows[0]?.find('.v-list-item-title').text()).toBe('Maison de Tante Agathe')
    expect(plain(rows[0]?.find('.details').text() ?? '')).toBe(
      '100 m radius · €0.2142/kWh since 1 Aug 2026 · 3 time windows · also for charges without a position',
    )
    expect(plain(rows[1]?.find('.details').text() ?? '')).toBe(
      '250 m radius · €0.00/kWh since 1 Jan 2026',
    )
    expect(rows[0]?.find('a').attributes('href')).toBe(`/settings/places/${place.id}`)
    expect(w.find('a.new-place').attributes('href')).toBe('/settings/places/new')
  })

  it('shows a price to come, and a price without a currency', async () => {
    fetchPlaces.mockResolvedValue([
      { ...place, tariff: [{ valid_from: '2026-12-01', price_per_kwh: 0.25, windows: [] }] },
    ])
    const w = await list(null)
    expect(plain(w.find('.details').text())).toContain('0.25/kWh from 1 Dec 2026')
  })

  it('shows no price for a place without one', async () => {
    fetchPlaces.mockResolvedValue([{ ...place, tariff: [] }])
    const w = await list()
    expect(plain(w.find('.details').text())).toBe(
      '100 m radius · also for charges without a position',
    )
  })

  it('says there is none yet', async () => {
    fetchPlaces.mockResolvedValue([])
    const w = await list()
    expect(w.find('.empty').text()).toBe('No place yet.')
    expect(w.find('.new-place').exists()).toBe(true)
  })

  it('stops adding at the limit of places, and says why', async () => {
    fetchPlaces.mockResolvedValue(
      Array.from({ length: settings.limits.places }, (_, i) => ({ ...place, id: `p${i}` })),
    )
    const w = await list()
    const button = w.find('.new-place')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('href')).toBeUndefined()
    expect(button.attributes('aria-describedby')).toBe('places-full')
    expect(w.find('#places-full').text()).toBe(
      `An account has at most ${settings.limits.places} places: delete one to add another.`,
    )
  })

  it('stops at the limit it is given', async () => {
    fetchPlaces.mockResolvedValue(places.items)
    const w = await list(eur, 2)
    expect(w.find('.new-place').attributes('disabled')).toBeDefined()
    expect(w.find('#places-full').text()).toContain('at most 2 places')
  })

  it('tells a failure', async () => {
    fetchPlaces.mockRejectedValue(new ApiError(500, 'internal', ''))
    const w = await list()
    expect(w.find('.v-alert').text()).toBe('The places could not be loaded. Try again in a moment.')
  })
})
