import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { ChargePage } from '@/pages/charge'
import { ApiError } from '@/shared/api'
import charging from '@fixtures/charging.json'
import settings from '@fixtures/settings.json'
import { mountWith, testRouter } from '@test/utils'

const { fetchCharge, fetchChargeInProgress, fetchSettings } = vi.hoisted(() => ({
  fetchCharge: vi.fn(),
  fetchChargeInProgress: vi.fn(),
  fetchSettings: vi.fn(),
}))
// The curve has its own tests: here, only that the page loads it.
vi.mock('@/widgets/charge-curve', () => ({
  ChargeCurve: defineComponent({
    name: 'ChargeCurve',
    props: {
      vehicle: { type: String, required: true },
      id: { type: String, required: true },
      live: { type: Object, default: null },
    },
    render: () => null,
  }),
}))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  fetchSettings,
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchCharge,
  fetchChargeInProgress,
}))

beforeEach(() => {
  fetchCharge.mockReset()
  fetchChargeInProgress.mockReset()
  fetchChargeInProgress.mockResolvedValue(null)
  fetchSettings.mockResolvedValue(settings)
})

async function mounted(id: string, query = '') {
  const router = testRouter()
  await router.push(`/vehicles/v1/charges/${id}${query}`)
  const { wrapper } = mountWith(ChargePage, { router, props: { vehicle: 'v1', id } })
  await flushPromises()
  return wrapper
}

describe('ChargePage', () => {
  it('tells an unknown charge, with a link to the list and its period', async () => {
    fetchCharge.mockRejectedValue(new ApiError(404, 'not_found', ''))
    const wrapper = await mounted('nope', '?to=2026-09-30')
    expect(wrapper.find('.v-alert').text()).toBe('This charge does not exist.')
    expect(wrapper.find('a.back').attributes('href')).toBe('/vehicles/v1/charges?to=2026-09-30')
    // The place too, chosen in the list.
    const atPlace = await mounted('nope', '?to=2026-09-30&place=outside')
    expect(atPlace.find('a.back').attributes('href')).toBe(
      '/vehicles/v1/charges?to=2026-09-30&place=outside',
    )
    expect(wrapper.find('.v-tab[aria-current="page"]').text()).toBe('Charges')
    expect(wrapper.findComponent({ name: 'ChargeCurve' }).props()).toEqual({
      vehicle: 'v1',
      id: 'nope',
      live: null,
    })
  })

  // Without the charge in progress, an ID not found may be that of a charge still open:
  // never said not to exist.
  it('tells an error, not an unknown charge, when the charge in progress fails', async () => {
    fetchChargeInProgress.mockRejectedValue(new ApiError(500, 'internal', ''))
    fetchCharge.mockRejectedValue(new ApiError(404, 'not_found', ''))
    const wrapper = await mounted('2026-09-28T22:40:00.000000Z')
    expect(wrapper.find('.not-found').exists()).toBe(false)
    expect(wrapper.find('.v-alert').text()).toBe(
      'The charge could not be loaded. Try again in a moment.',
    )
  })

  // The charge in progress has the ID it will have once over: the same page follows it.
  it('follows the charge in progress of its ID, live', async () => {
    fetchChargeInProgress.mockResolvedValue(charging.charge)
    const wrapper = await mounted(charging.charge?.id ?? '')
    expect(fetchCharge).not.toHaveBeenCalled()
    expect(wrapper.find('.charge-in-progress h1').text()).toBe('Charge in progress')
    expect(wrapper.findComponent({ name: 'ChargeCurve' }).props('live')).toEqual(charging.charge)
  })
})
