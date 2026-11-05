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

  it('lists no journey driven once, and shows nothing without a trip', async () => {
    const once = structuredClone(fixture) as Map
    once.links = [{ ...fixture.links[0], trips: 1 } as Map['links'][number]]
    expect((await mount(once)).find('.frequent').exists()).toBe(false)

    const none: Map = { ...(fixture as Map), trips: 0, spots: [], links: [] }
    expect((await mount(none)).find('.trip-map').exists()).toBe(false)
  })
})
