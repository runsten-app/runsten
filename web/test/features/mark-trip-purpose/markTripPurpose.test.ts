import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  MarkSelectionBar,
  PurposeFilter,
  PurposeMenu,
  PurposePicker,
  purposeQuery,
  usePurposeFilter,
} from '@/features/mark-trip-purpose'
import { ApiError } from '@/shared/api'
import trip from '@fixtures/trip.json'
import { mountWith, testRouter, withSetup } from '@test/utils'

const { setTripMarks } = vi.hoisted(() => ({ setTripMarks: vi.fn() }))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  setTripMarks,
}))

beforeEach(() => {
  setTripMarks.mockReset()
  setTripMarks.mockResolvedValue(undefined)
})

const vehicle = 'v1'
const work = { id: '5e1f0000-0000-4000-8000-000000000002', name: 'Work' }
const byRule = { ...trip, purpose: 'commute', purpose_origin: 'rule', purpose_place: work }
const byHand = { ...trip, purpose: 'business', purpose_origin: 'hand', purpose_place: null }
const noneByHand = { ...trip, purpose: null, purpose_origin: 'hand', purpose_place: null }

const overlayItems = () =>
  [...document.querySelectorAll('.v-overlay--active .v-list-item-title')].map((e) => e.textContent)

describe('purposeQuery', () => {
  it('keeps a purpose the API takes, or none, and nothing else', () => {
    expect(purposeQuery({ purpose: 'business' })).toBe('business')
    expect(purposeQuery({ purpose: 'none' })).toBe('none')
    expect(purposeQuery({ purpose: 'work' })).toBeUndefined()
    expect(purposeQuery({ purpose: ['business'] })).toBeUndefined()
    expect(purposeQuery({})).toBeUndefined()
  })
})

describe('usePurposeFilter', () => {
  it('keeps the purpose in the URL, beside the period', async () => {
    const router = testRouter()
    await router.push(`/vehicles/${vehicle}/trips?from=2026-09-01`)
    const { result } = withSetup(() => usePurposeFilter(), { router })
    await result.setPurpose('commute')
    expect(router.currentRoute.value.query).toEqual({ from: '2026-09-01', purpose: 'commute' })
    expect(result.purpose.value).toBe('commute')
    await result.setPurpose(undefined)
    expect(router.currentRoute.value.query).toEqual({ from: '2026-09-01' })
  })
})

describe('PurposeFilter', () => {
  it('offers every purpose, and the trips without one, the URL’s chosen', async () => {
    const router = testRouter()
    await router.push(`/vehicles/${vehicle}/trips?purpose=none`)
    const { wrapper } = mountWith(PurposeFilter, { router })
    await flushPromises()
    const chips = wrapper.findAll('.v-chip')
    expect(chips.map((c) => c.text())).toEqual([
      'All',
      'Business',
      'Commute',
      'Personal',
      'Unmarked',
    ])
    expect(wrapper.find('.purpose-filter-selected').text()).toBe('Unmarked')
    await chips[1]?.trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query).toEqual({ purpose: 'business' }))
    await chips[0]?.trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query).toEqual({}))
  })
})

describe('PurposeMenu', () => {
  async function mounted(t: object) {
    const { wrapper, queryClient } = mountWith(PurposeMenu, {
      props: { vehicle, trip: t },
      attachTo: document.body,
    })
    await flushPromises()
    return { wrapper, queryClient }
  }

  it('shows a purpose given by a rule as such, and changes it by hand', async () => {
    const { wrapper, queryClient } = await mounted(byRule)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const button = wrapper.find('.purpose-menu')
    expect(button.text()).toBe('Commute auto')
    expect(button.attributes('title')).toBe('From the rule of Work')
    expect(button.attributes('aria-label')).toBe('Purpose of the trip: Commute')
    await button.trigger('click')
    await flushPromises()
    expect(overlayItems()).toEqual(['Business', 'Commute', 'Personal', 'None'])
    ;(document.querySelectorAll('.v-overlay--active .v-list-item')[0] as HTMLElement).click()
    await flushPromises()
    expect(setTripMarks).toHaveBeenCalledWith(vehicle, [trip.id], 'business')
    expect(invalidate.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)).toEqual([
      ['trips', vehicle],
      ['trip'],
      ['stats'],
    ])
    wrapper.unmount()
  })

  it('offers a mark by hand to go back to the rule', async () => {
    const { wrapper } = await mounted(byHand)
    const button = wrapper.find('.purpose-menu')
    expect(button.text()).toBe('Business')
    expect(button.attributes('title')).toBe('Marked by hand')
    await button.trigger('click')
    await flushPromises()
    expect(overlayItems()).toEqual(['Business', 'Commute', 'Personal', 'Back to the rule'])
    ;(document.querySelectorAll('.v-overlay--active .v-list-item')[3] as HTMLElement).click()
    await flushPromises()
    expect(setTripMarks).toHaveBeenCalledWith(vehicle, [trip.id], 'rule')
    wrapper.unmount()
  })

  it('asks for a purpose without one, and says none by hand', async () => {
    const { wrapper } = await mounted(trip)
    expect(wrapper.find('.purpose-menu').text()).toBe('Purpose')
    wrapper.unmount()
    const none = await mounted(noneByHand)
    expect(none.wrapper.find('.purpose-menu').text()).toBe('None')
    none.wrapper.unmount()
  })

  it('turns red when the mark fails', async () => {
    setTripMarks.mockRejectedValue(new ApiError(500, 'internal', 'boom'))
    const { wrapper } = await mounted(trip)
    await wrapper.find('.purpose-menu').trigger('click')
    await flushPromises()
    ;(document.querySelectorAll('.v-overlay--active .v-list-item')[2] as HTMLElement).click()
    await flushPromises()
    expect(wrapper.find('.purpose-menu').classes()).toContain('text-error')
    wrapper.unmount()
  })
})

describe('PurposePicker', () => {
  async function mounted(t: object) {
    const { wrapper } = mountWith(PurposePicker, { props: { vehicle, trip: t } })
    await flushPromises()
    return wrapper
  }

  it('tells where the purpose comes from, and marks another by hand', async () => {
    const w = await mounted(byRule)
    expect(w.find('.help').text()).toBe(
      'From the rule of Work: Commute. Choose another to override it.',
    )
    expect(w.find('.v-btn--active').text()).toBe('Commute')
    const buttons = w.findAll('.v-btn-toggle .v-btn')
    expect(buttons.map((b) => b.text())).toEqual(['Business', 'Commute', 'Personal', 'None'])
    await buttons[1]?.trigger('click') // the same: nothing written
    await buttons[3]?.trigger('click')
    await flushPromises()
    expect(setTripMarks.mock.calls).toEqual([[vehicle, [trip.id], 'none']])
  })

  it('goes back to the rule from a mark by hand', async () => {
    const w = await mounted(noneByHand)
    expect(w.find('.help').text()).toContain('Marked by hand.')
    expect(w.find('.v-btn--active').text()).toBe('None')
    await w.find('.back-to-rule').trigger('click')
    await flushPromises()
    expect(setTripMarks).toHaveBeenCalledWith(vehicle, [trip.id], 'rule')
  })

  it('says no rule applies, and tells a failure', async () => {
    setTripMarks.mockRejectedValue(new ApiError(500, 'internal', 'boom'))
    const w = await mounted(trip)
    expect(w.find('.help').text()).toBe("No place's rule applies to this trip.")
    expect(w.find('.v-btn--active').exists()).toBe(false)
    await w.findAll('.v-btn-toggle .v-btn')[0]?.trigger('click')
    await flushPromises()
    expect(w.find('.v-alert').text()).toBe('The trips could not be marked. Try again.')
  })
})

describe('MarkSelectionBar', () => {
  it('marks the trips selected at once, then says it is done', async () => {
    const { wrapper } = mountWith(MarkSelectionBar, { props: { vehicle, selected: ['a', 'b'] } })
    await flushPromises()
    const bar = wrapper.findComponent(MarkSelectionBar)
    expect(wrapper.text()).toContain('2 trips selected')
    const buttons = wrapper.findAll('.v-btn')
    expect(buttons.map((b) => b.text())).toEqual([
      'Business',
      'Commute',
      'Personal',
      'Clear the mark',
    ])
    await buttons[2]?.trigger('click')
    await flushPromises()
    expect(setTripMarks).toHaveBeenCalledWith(vehicle, ['a', 'b'], 'personal')
    expect(bar.emitted('done')).toHaveLength(1)
    await buttons[3]?.trigger('click')
    await flushPromises()
    expect(setTripMarks).toHaveBeenLastCalledWith(vehicle, ['a', 'b'], 'rule')
  })

  it('tells a failure, and stays', async () => {
    setTripMarks.mockRejectedValue(new ApiError(500, 'internal', 'boom'))
    const { wrapper } = mountWith(MarkSelectionBar, { props: { vehicle, selected: ['a'] } })
    await wrapper.findAll('.v-btn')[0]?.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('1 trip selected')
    expect(wrapper.find('.v-alert').text()).toBe('The trips could not be marked. Try again.')
    expect(wrapper.findComponent(MarkSelectionBar).emitted('done')).toBeUndefined()
  })

  it('marks nothing while nothing is selected', async () => {
    const { wrapper } = mountWith(MarkSelectionBar, { props: { vehicle, selected: [] } })
    expect(wrapper.findAll('.v-btn').every((b) => b.attributes('disabled') !== undefined)).toBe(
      true,
    )
  })
})
