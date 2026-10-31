import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PlaceSelect, placeQuery } from '@/features/filter-place'
import places from '@fixtures/places.json'
import { mountWith, testRouter } from '@test/utils'

const { fetchPlaces } = vi.hoisted(() => ({ fetchPlaces: vi.fn() }))
vi.mock('@/entities/place', async (original) => ({
  ...(await original<typeof import('@/entities/place')>()),
  fetchPlaces,
}))

beforeEach(() => {
  fetchPlaces.mockReset()
  fetchPlaces.mockResolvedValue(places.items)
})

const [home, work] = places.items

async function mounted(query: string) {
  const router = testRouter()
  await router.push(`/vehicles/v1/charges${query}`)
  const { wrapper } = mountWith(PlaceSelect, { router, attachTo: document.body })
  await flushPromises()
  return { wrapper, router }
}

// options opens the select and reads its items.
async function options(w: Awaited<ReturnType<typeof mounted>>['wrapper']) {
  await w.find('.place-select .v-field').trigger('mousedown')
  await flushPromises()
  return [...document.querySelectorAll('.v-overlay .v-list-item-title')].map((e) => e.textContent)
}

describe('PlaceSelect', () => {
  it('offers every place, the account’s by name, then outside them and without a position', async () => {
    const { wrapper } = await mounted('?from=2026-09-01')
    expect(wrapper.find('.place-select').text()).toContain('All places')
    expect(await options(wrapper)).toEqual([
      'All places',
      'Maison de Tante Agathe',
      'Work',
      'Elsewhere',
      'No position',
    ])
    wrapper.unmount()
  })

  it('keeps the place chosen in the URL, beside the period', async () => {
    const { wrapper, router } = await mounted('?from=2026-09-01')
    const select = wrapper.findComponent({ name: 'VSelect' })
    select.vm.$emit('update:modelValue', work?.id)
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toEqual({ from: '2026-09-01', place: work?.id }),
    )
    await flushPromises()
    expect(wrapper.find('.place-select').text()).toContain('Work')
    select.vm.$emit('update:modelValue', 'any')
    await vi.waitFor(() => expect(router.currentRoute.value.query).toEqual({ from: '2026-09-01' }))
    wrapper.unmount()
  })

  it('names a place since deleted, from an old link', async () => {
    const { wrapper } = await mounted('?place=00000000-0000-4000-8000-000000000000')
    expect(wrapper.find('.place-select').text()).toContain('A deleted place')
    expect(await options(wrapper)).toContain('A deleted place')
    wrapper.unmount()
  })

  it('shows the place of the URL', async () => {
    const { wrapper } = await mounted(`?place=${home?.id}`)
    expect(wrapper.find('.place-select').text()).toContain('Maison de Tante Agathe')
    wrapper.unmount()
  })
})

describe('placeQuery', () => {
  it('keeps a place the API takes, and nothing else', () => {
    expect(placeQuery({ place: 'outside' })).toEqual({ place: 'outside' })
    expect(placeQuery({ place: 'no_position' })).toEqual({ place: 'no_position' })
    expect(placeQuery({ place: home?.id ?? '' })).toEqual({ place: home?.id })
    expect(placeQuery({ place: 'home' })).toEqual({})
    expect(placeQuery({ place: ['outside'] })).toEqual({})
    expect(placeQuery({})).toEqual({})
  })
})
