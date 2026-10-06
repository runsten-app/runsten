import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { BucketToggle, useBucket } from '@/features/view-stats'
import type { Days } from '@/shared/lib'
import { mountWith, testRouter, withSetup } from '@test/utils'

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 26, 10))
})
afterEach(() => {
  vi.useRealTimers()
})

const month: Days = { from: '2026-09-01', to: '2026-09-30' }
const year: Days = { from: '2025-10-01', to: '2026-09-30' }
const long: Days = { from: '2024-01-01', to: '2026-09-30' }

async function bucketOf(query: string, days: Days) {
  const router = testRouter()
  await router.push(`/vehicles/v1/stats${query}`)
  return withSetup(() => useBucket(days), { router })
}

describe('useBucket', () => {
  it.each<[string, Days, string]>([
    ['', month, 'day'], // chosen by the length
    ['', year, 'month'],
    ['?bucket=week', month, 'week'], // the URL's
    ['?bucket=year', month, 'day'], // not a split
    ['?bucket=day', long, 'month'], // over 400 days: never asked for
  ])('%s over %j: %s', async (query, days, want) => {
    const { result } = await bucketOf(query, days)
    expect(result.bucket.value).toBe(want)
  })

  it('writes the split to the URL, and nothing else', async () => {
    const { result, router } = await bucketOf('?from=2026-09-01', month)
    await result.setBucket('week')
    expect(router.currentRoute.value.query).toEqual({ from: '2026-09-01', bucket: 'week' })
    await result.setBucket('year' as never)
    expect(router.currentRoute.value.query.bucket).toBe('week')
  })
})

describe('BucketToggle', () => {
  it('is a group of three, the split shown pressed, one too fine disabled', async () => {
    const router = testRouter()
    await router.push('/vehicles/v1/stats')
    const { wrapper } = mountWith(BucketToggle, { router, props: { days: long } })
    await flushPromises()
    expect(wrapper.find('[role="group"]').attributes('aria-label')).toBe('Split')
    const buttons = wrapper.findAll('button')
    expect(buttons.map((b) => b.text())).toEqual(['By day', 'By week', 'By month'])
    expect(buttons.map((b) => b.attributes('aria-pressed'))).toEqual(['false', 'false', 'true'])
    expect(buttons[0]?.attributes('disabled')).toBeDefined()

    await buttons[1]?.trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query).toEqual({ bucket: 'week' }))
  })
})
