import { beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'
import { useDeletePolestarCredential, useSetPolestarCredential } from '@/features/manage-polestar'
import connection from '@fixtures/connection-polestar.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({
  setPolestarCredential: vi.fn(),
  deletePolestarCredential: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

// What a credential changes: the connection, and each vehicle's state of reading.
const stale = [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()]
const keys = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)
const body = {
  client_id: 'runsten-dev',
  client_secret: 'runsten-dev-secret',
  account_id: 'acct-1234',
}

describe('useSetPolestarCredential', () => {
  it('gives the credential, keeps the connection it gives back, and refreshes the vehicles', async () => {
    api.setPolestarCredential.mockResolvedValue(connection)
    const { result, queryClient } = withSetup(useSetPolestarCredential)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(await result.setCredential(body)).toEqual(connection)
    expect(api.setPolestarCredential).toHaveBeenCalledWith(body)
    expect(queryClient.getQueryData(connectionKeys.current())).toEqual(connection)
    expect(keys(invalidate)).toEqual(stale)
  })
})

describe('useDeletePolestarCredential', () => {
  it('removes the credential and refreshes what it changes', async () => {
    api.deletePolestarCredential.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(useDeletePolestarCredential)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.deleteCredential()
    expect(api.deletePolestarCredential).toHaveBeenCalled()
    expect(keys(invalidate)).toEqual(stale)
  })
})
