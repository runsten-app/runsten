import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PeriodPicker } from '@/features/filter-period'
import { mountWith, testRouter } from '@test/utils'

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 6, 10))
})
afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
})

async function mounted(query: string, props: Record<string, unknown> = {}) {
  const router = testRouter()
  await router.push(`/vehicles/v1/trips${query}`)
  const { wrapper } = mountWith(PeriodPicker, { router, props, attachTo: document.body })
  await flushPromises()
  return { wrapper, router }
}

const label = (w: Awaited<ReturnType<typeof mounted>>['wrapper']) =>
  w.find('.current').text().replace(/\s+/g, ' ').trim()

describe('PeriodPicker', () => {
  it('names the period, its kind first', async () => {
    const cases: [string, string][] = [
      ['', 'Period: Whole history'],
      ['?to=2026-10-06', 'Period: Whole history'],
      ['?from=2026-10-01&to=2026-10-31', 'Period: October 2026'],
      ['?from=2026-01-01&to=2026-12-31', 'Period: 2026'],
      ['?from=2025-11-01&to=2026-10-31', 'Period: Last 12 months'],
      ['?from=2026-10-03&to=2026-10-17', 'Period: 3 – 17 Oct 2026'],
      ['?from=2026-10-03&to=2026-10-03', 'Period: 3 Oct 2026'],
      ['?from=2026-10-03', 'Period: Since 3 Oct 2026'],
      ['?to=2026-10-03', 'Period: Until 3 Oct 2026'],
    ]
    for (const [query, name] of cases) {
      const { wrapper } = await mounted(query)
      expect(label(wrapper)).toBe(name)
      wrapper.unmount()
    }
  })

  it('steps to the period before or after, of the same length', async () => {
    const { wrapper, router } = await mounted('?from=2026-10-01&to=2026-10-31&view=costs')
    expect(wrapper.find('.previous').attributes('aria-label')).toBe('Previous period')
    await wrapper.find('.previous').trigger('click')
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toEqual({
        from: '2026-09-01',
        to: '2026-09-30',
        view: 'costs',
      }),
    )
    await wrapper.find('.next').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query.from).toBe('2026-10-01'))
    await flushPromises()
    await wrapper.find('.next').trigger('click')
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toMatchObject({
        from: '2026-11-01',
        to: '2026-11-30',
      }),
    )
  })

  it('has no step for a period open at an end', async () => {
    const { wrapper } = await mounted('?from=2026-10-03')
    expect(wrapper.find('.previous').exists()).toBe(false)
    expect(wrapper.find('.next').exists()).toBe(false)
  })

  it('opens the usual periods and the days, and closes on a shortcut', async () => {
    const { wrapper, router } = await mounted('?from=2026-10-01&to=2026-10-31', {
      clearable: false,
    })
    await wrapper.find('.current').trigger('click')
    await flushPromises()
    const menu = document.querySelector('.period-menu')
    expect(menu?.getAttribute('role')).toBe('dialog')
    expect(menu?.querySelectorAll('input[type="date"]')).toHaveLength(2)
    // Without clearable, nothing empties the days.
    expect(menu?.querySelector('.clear')).toBeNull()
    const pressed = menu?.querySelector('.period-shortcuts [aria-pressed="true"]')
    expect(pressed?.textContent?.trim()).toBe('This month')
    const year = [...(menu?.querySelectorAll<HTMLElement>('.period-shortcuts button') ?? [])].find(
      (b) => b.textContent?.trim() === 'This year',
    )
    year?.click()
    await vi.waitFor(() =>
      expect(router.currentRoute.value.query).toEqual({ from: '2026-01-01', to: '2026-12-31' }),
    )
    await flushPromises()
    expect(label(wrapper)).toBe('Period: 2026')
    await vi.waitFor(() =>
      expect(document.querySelector('.period-menu')?.closest('.v-overlay--active')).toBeFalsy(),
    )
  })

  it('tells an invalid period under the line', async () => {
    const { wrapper } = await mounted('?from=2026-09-30&to=2026-09-01')
    expect(wrapper.find('.period-error').text()).toBe('The end comes before the start.')
    expect(label(wrapper)).toBe('Period: Choose a period')
    expect(wrapper.find('.previous').exists()).toBe(false)
  })
})
