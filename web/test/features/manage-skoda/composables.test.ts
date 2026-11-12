import { beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'
import { useDeleteSkodaCredential, useSetSkodaCredential } from '@/features/manage-skoda'
import connection from '@fixtures/connection-skoda.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({
  setSkodaCredential: vi.fn(),
  deleteSkodaCredential: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

// What a key changes: the connection, and each vehicle's state of reading.
const stale = [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()]
const keys = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)
const body = { api_key: 'runsten-dev-skoda-key', vins: ['TMBJC9NY0RF000001'] }

describe('useSetSkodaCredential', () => {
  it('gives the key with its VINs, keeps the connection it gives back, and refreshes the vehicles', async () => {
    api.setSkodaCredential.mockResolvedValue(connection)
    const { result, queryClient } = withSetup(() => useSetSkodaCredential())
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(await result.setCredential(body)).toEqual(connection)
    expect(api.setSkodaCredential).toHaveBeenCalledWith(body, expect.anything())
    expect(queryClient.getQueryData(connectionKeys.current())).toEqual(connection)
    expect(keys(invalidate)).toEqual(stale)
  })
})

describe('useDeleteSkodaCredential', () => {
  it('removes the key and refreshes the vehicles', async () => {
    api.deleteSkodaCredential.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(() => useDeleteSkodaCredential())
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.deleteCredential()
    expect(api.deleteSkodaCredential).toHaveBeenCalled()
    expect(keys(invalidate)).toEqual(stale)
  })
})
