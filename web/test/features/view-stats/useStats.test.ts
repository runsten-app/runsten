import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { statsKeys } from '@/entities/stats'
import { useStats } from '@/features/view-stats'
import totals from '@fixtures/stats-totals.json'
import { withSetup } from '@test/utils'

const mocks = vi.hoisted(() => ({ fetchStats: vi.fn() }))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  ...mocks,
}))

beforeEach(() => {
  mocks.fetchStats.mockReset()
})

const vehicle = 'v1'

describe('useStats', () => {
  it("reads the period in the reader's time zone", async () => {
    mocks.fetchStats.mockResolvedValue(totals)
    const { result, queryClient } = withSetup(() =>
      useStats(vehicle, '2026-09-28T00:00:00.000Z', undefined),
    )
    await flushPromises()
    const q = { from: '2026-09-28T00:00:00.000Z', to: undefined, tz: 'UTC', bucket: undefined }
    expect(mocks.fetchStats).toHaveBeenCalledWith(vehicle, q)
    expect(result.stats.value).toEqual(totals)
    expect(queryClient.getQueryData(statsKeys.period(vehicle, q))).toEqual(totals)
  })

  it('reads again when the period or the split changes', async () => {
    mocks.fetchStats.mockResolvedValue(totals)
    const to = ref<string | undefined>(undefined)
    const bucket = ref<'day' | undefined>(undefined)
    withSetup(() => useStats(vehicle, undefined, to, bucket))
    await flushPromises()
    to.value = '2026-09-29T00:00:00.000Z'
    await flushPromises()
    bucket.value = 'day'
    await flushPromises()
    expect(mocks.fetchStats).toHaveBeenCalledTimes(3)
    expect(mocks.fetchStats).toHaveBeenLastCalledWith(vehicle, {
      from: undefined,
      to: to.value,
      tz: 'UTC',
      bucket: 'day',
    })
  })
})
