import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { batteryKeys } from '@/entities/battery'
import { useBattery } from '@/features/view-battery'
import battery from '@fixtures/battery.json'
import { withSetup } from '@test/utils'

const mocks = vi.hoisted(() => ({ getBattery: vi.fn() }))
vi.mock('@/entities/battery', async (original) => ({
  ...(await original<typeof import('@/entities/battery')>()),
  ...mocks,
}))

beforeEach(() => {
  mocks.getBattery.mockReset()
})

const vehicle = 'v1'

describe('useBattery', () => {
  it("reads the whole history in the reader's time zone", async () => {
    mocks.getBattery.mockResolvedValue(battery)
    const { result, queryClient } = withSetup(() => useBattery(vehicle))
    await flushPromises()
    expect(mocks.getBattery).toHaveBeenCalledWith(vehicle, { tz: 'UTC' })
    expect(result.battery.value).toEqual(battery)
    expect(queryClient.getQueryData(batteryKeys.trend(vehicle, { tz: 'UTC' }))).toEqual(battery)
  })
})
