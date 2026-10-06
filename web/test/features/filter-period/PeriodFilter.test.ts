import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { PeriodFilter, periodQuery } from '@/features/filter-period'
import { mountWith, testRouter } from '@test/utils'

async function mounted(path: string, props: Record<string, unknown> = {}) {
  const router = testRouter()
  await router.push(path)
  const { wrapper } = mountWith(PeriodFilter, { router, props })
  await flushPromises()
  return { wrapper, router }
}

const field = (w: Awaited<ReturnType<typeof mounted>>['wrapper'], name: string) =>
  w.find(`input[name="${name}"]`)

describe('PeriodFilter', () => {
  it('reads the period from the URL', async () => {
    const { wrapper } = await mounted('/vehicles/v1/trips?from=2026-09-01&to=2026-09-30')
    expect((field(wrapper, 'from').element as HTMLInputElement).value).toBe('2026-09-01')
    expect((field(wrapper, 'to').element as HTMLInputElement).value).toBe('2026-09-30')
    expect(field(wrapper, 'from').attributes('type')).toBe('date')
    expect(field(wrapper, 'to').attributes('min')).toBe('2026-09-01')
  })

  it('writes it to the URL, keeping the rest of the query', async () => {
    const { wrapper, router } = await mounted('/vehicles/v1/trips?to=2026-09-30&x=1')
    await field(wrapper, 'from').setValue('2026-09-01')
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toEqual({
        from: '2026-09-01',
        to: '2026-09-30',
        x: '1',
      }),
    )
    expect(router.currentRoute.value.path).toBe('/vehicles/v1/trips')
  })

  it('clears it', async () => {
    const { wrapper, router } = await mounted('/vehicles/v1/trips?from=2026-09-01&to=2026-09-30')
    await wrapper.find('button.clear').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/vehicles/v1/trips'))
    expect(wrapper.find('button.clear').exists()).toBe(false)
  })

  it('offers no clearing when not clearable', async () => {
    const { wrapper } = await mounted('/vehicles/v1/stats?from=2026-09-01&to=2026-09-30', {
      clearable: false,
    })
    expect(wrapper.find('button.clear').exists()).toBe(false)
  })

  it.each([
    ['?from=2026-09-30&to=2026-09-01', 'to', 'The end comes before the start.'],
    ['?from=someday', 'from', 'Not a valid date.'],
    ['?to=2026-13-01', 'to', 'Not a valid date.'],
  ])('tells what is wrong with %s', async (query, name, message) => {
    const { wrapper } = await mounted(`/vehicles/v1/trips${query}`)
    const input = wrapper
      .findAllComponents({ name: 'VTextField' })
      .find((c) => c.props('name') === name)
    expect(input?.text()).toContain(message)
  })
})

describe('periodQuery', () => {
  it('keeps the dates of a query only', () => {
    expect(periodQuery({ from: '2026-09-01', to: '', x: '1' })).toEqual({ from: '2026-09-01' })
    expect(periodQuery({ from: ['a', 'b'], to: null })).toEqual({})
  })
})
