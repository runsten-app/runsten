import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'
import { useConnectionSettings, useDeleteApiKey, useSetApiKey } from '@/features/manage-api-key'
import connection from '@fixtures/connection.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchConnection: vi.fn(),
  setApiKey: vi.fn(),
  deleteApiKey: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

// What a key changes: the connection, and each vehicle's key and collection.
const stale = [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()]
const keys = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)

describe('useConnectionSettings', () => {
  it('reads the connection', async () => {
    api.fetchConnection.mockResolvedValue(connection)
    const { result } = withSetup(useConnectionSettings)
    await flushPromises()
    expect(result.connection.value).toEqual(connection)
  })
})

describe('useSetApiKey', () => {
  it('gives the key, keeps the connection it gives back, and refreshes the vehicles', async () => {
    api.setApiKey.mockResolvedValue(connection)
    const { result, queryClient } = withSetup(useSetApiKey)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(await result.setApiKey('0123456789abcdef0123456789abcdef')).toEqual(connection)
    expect(api.setApiKey).toHaveBeenCalledWith('0123456789abcdef0123456789abcdef')
    expect(queryClient.getQueryData(connectionKeys.current())).toEqual(connection)
    expect(keys(invalidate)).toEqual(stale)
  })
})

describe('useDeleteApiKey', () => {
  it('removes the key and refreshes what it changes', async () => {
    api.deleteApiKey.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(useDeleteApiKey)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.deleteApiKey()
    expect(api.deleteApiKey).toHaveBeenCalled()
    expect(keys(invalidate)).toEqual(stale)
  })
})
