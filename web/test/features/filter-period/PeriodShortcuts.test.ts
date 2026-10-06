import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PeriodShortcuts } from '@/features/filter-period'
import { mountWith, testRouter } from '@test/utils'

// Only Date: the promises of the router keep running.
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 26, 10))
})
afterEach(() => {
  vi.useRealTimers()
})

async function mounted(path: string) {
  const router = testRouter()
  await router.push(path)
  const { wrapper } = mountWith(PeriodShortcuts, { router })
  await flushPromises()
  return { wrapper, router }
}

describe('PeriodShortcuts', () => {
  it('tells which one is the period shown', async () => {
    const { wrapper } = await mounted('/vehicles/v1/stats?from=2026-09-01&to=2026-09-30')
    expect(wrapper.find('[role="group"]').attributes('aria-label')).toBe('Period')
    const pressed = wrapper.findAll('button').map((b) => [b.text(), b.attributes('aria-pressed')])
    expect(pressed).toEqual([
      ['This month', 'true'],
      ['Last month', 'false'],
      ['This year', 'false'],
      ['Last 12 months', 'false'],
      ['All', 'false'],
    ])
  })

  it('sets its period in the URL, keeping the rest of the query', async () => {
    const { wrapper, router } = await mounted('/vehicles/v1/stats?bucket=week')
    const button = (text: string) => wrapper.findAll('button').find((b) => b.text() === text)
    await button('Last 12 months')?.trigger('click')
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toEqual({
        from: '2025-10-01',
        to: '2026-09-30',
        bucket: 'week',
      }),
    )
    await button('All')?.trigger('click')
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toEqual({ to: '2026-09-26', bucket: 'week' }),
    )
    await flushPromises()
    expect(button('All')?.attributes('aria-pressed')).toBe('true')
  })
})
