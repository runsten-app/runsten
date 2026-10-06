import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { ChargePage } from '@/pages/charge'
import { ApiError } from '@/shared/api'
import { mountWith, testRouter } from '@test/utils'

const { fetchCharge } = vi.hoisted(() => ({ fetchCharge: vi.fn() }))
// The curve has its own tests: here, only that the page loads it.
vi.mock('@/widgets/charge-curve', () => ({
  ChargeCurve: defineComponent({
    name: 'ChargeCurve',
    props: { vehicle: { type: String, required: true }, id: { type: String, required: true } },
    render: () => null,
  }),
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchCharge,
}))

beforeEach(() => {
  fetchCharge.mockReset()
})

describe('ChargePage', () => {
  it('tells an unknown charge, with a link to the list and its period', async () => {
    fetchCharge.mockRejectedValue(new ApiError(404, 'not_found', ''))
    const router = testRouter()
    await router.push('/vehicles/v1/charges/nope?to=2026-09-30')
    const { wrapper } = mountWith(ChargePage, { router, props: { vehicle: 'v1', id: 'nope' } })
    await flushPromises()
    expect(wrapper.find('.v-alert').text()).toBe('This charge does not exist.')
    expect(wrapper.find('a.back').attributes('href')).toBe('/vehicles/v1/charges?to=2026-09-30')
    expect(wrapper.find('.v-tab[aria-current="page"]').text()).toBe('Charges')
    expect(wrapper.findComponent({ name: 'ChargeCurve' }).props()).toEqual({
      vehicle: 'v1',
      id: 'nope',
    })
  })
})
