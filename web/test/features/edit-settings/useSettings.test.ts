import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { chargeKeys } from '@/entities/charge'
import { settingsKeys } from '@/entities/settings'
import { statsKeys } from '@/entities/stats'
import { useSetCurrency, useSettings } from '@/features/edit-settings'
import { ApiError } from '@/shared/api'
import settings from '@fixtures/settings.json'
import unset from '@fixtures/settings-unset.json'
import { withSetup } from '@test/utils'

const { fetchSettings, setCurrency } = vi.hoisted(() => ({
  fetchSettings: vi.fn(),
  setCurrency: vi.fn(),
}))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  fetchSettings,
  setCurrency,
}))

beforeEach(() => {
  fetchSettings.mockReset()
  setCurrency.mockReset()
})

describe('useSettings', () => {
  it('reads the settings', async () => {
    fetchSettings.mockResolvedValue(unset)
    const { result } = withSetup(useSettings)
    await flushPromises()
    expect(result.settings.value).toEqual(unset)
  })
})

describe('useSetCurrency', () => {
  it('caches the new settings and refreshes the costs', async () => {
    setCurrency.mockResolvedValue(settings)
    const { result, queryClient } = withSetup(useSetCurrency)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.setCurrency('EUR')
    expect(setCurrency).toHaveBeenCalledWith('EUR')
    expect(queryClient.getQueryData(settingsKeys.current())).toEqual(settings)
    const keys = invalidate.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)
    expect(keys).toEqual([chargeKeys.all(), chargeKeys.details(), statsKeys.every()])
  })

  it('keeps the settings when refused', async () => {
    setCurrency.mockRejectedValue(new ApiError(409, 'currency_in_use', ''))
    const { result, queryClient } = withSetup(useSetCurrency)
    queryClient.setQueryData(settingsKeys.current(), settings)
    await expect(result.setCurrency('SEK')).rejects.toMatchObject({ code: 'currency_in_use' })
    await flushPromises()
    expect(result.error.value).toMatchObject({ code: 'currency_in_use' })
    expect(queryClient.getQueryData(settingsKeys.current())).toEqual(settings)
  })
})
