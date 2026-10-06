import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { AxisToggle, useCapacityAxis } from '@/features/view-battery'
import { mountWith, testRouter, withSetup } from '@test/utils'

async function axisOf(query: string) {
  const router = testRouter()
  await router.push(`/vehicles/v1/battery${query}`)
  return withSetup(() => useCapacityAxis(), { router })
}

describe('useCapacityAxis', () => {
  it.each<[string, string]>([
    ['', 'date'],
    ['?axis=odometer', 'odometer'],
    ['?axis=garbage', 'date'], // not an axis
  ])('%s reads %s', async (query, want) => {
    const { result } = await axisOf(query)
    expect(result.axis.value).toBe(want)
  })

  it('writes the axis to the URL, and nothing else', async () => {
    const { result, router } = await axisOf('?from=2026-09-01')
    await result.setAxis('odometer')
    expect(router.currentRoute.value.query).toEqual({ from: '2026-09-01', axis: 'odometer' })
    await result.setAxis('garbage' as never)
    expect(router.currentRoute.value.query.axis).toBe('odometer')
  })
})

describe('AxisToggle', () => {
  it('is a labelled group of two, the axis shown pressed', async () => {
    const router = testRouter()
    await router.push('/vehicles/v1/battery')
    const { wrapper } = mountWith(AxisToggle, { router })
    await flushPromises()
    expect(wrapper.find('[role="group"]').attributes('aria-label')).toBe('Axis')
    const buttons = wrapper.findAll('button')
    expect(buttons.map((b) => b.text())).toEqual(['Date', 'Mileage'])
    expect(buttons.map((b) => b.attributes('aria-pressed'))).toEqual(['true', 'false'])

    // Only the URL's query changes: the focus stays on the button pressed.
    await buttons[1]?.trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query).toEqual({ axis: 'odometer' }))
    expect(buttons[1]?.attributes('aria-pressed')).toBe('true')
  })
})
