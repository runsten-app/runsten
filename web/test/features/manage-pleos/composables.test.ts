import { beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'
import { useDeletePleosCredential, useSetPleosCredential } from '@/features/manage-pleos'
import connection from '@fixtures/connection-pleos.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({
  setPleosCredential: vi.fn(),
  deletePleosCredential: vi.fn(),
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
const body = {
  client_id: 'runsten-dev',
  client_secret: 'runsten-dev-secret',
}

describe('useSetPleosCredential', () => {
  it('gives the key of the brand, keeps the connection it gives back, and refreshes the vehicles', async () => {
    api.setPleosCredential.mockResolvedValue(connection)
    const { result, queryClient } = withSetup(() => useSetPleosCredential('kia'))
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(await result.setCredential(body)).toEqual(connection)
    expect(api.setPleosCredential).toHaveBeenCalledWith('kia', body)
    expect(queryClient.getQueryData(connectionKeys.current())).toEqual(connection)
    expect(keys(invalidate)).toEqual(stale)
  })

  it('hands each brand its own key', async () => {
    api.setPleosCredential.mockResolvedValue(connection)
    const { result: hyundai } = withSetup(() => useSetPleosCredential('hyundai'))
    await hyundai.setCredential(body)
    expect(api.setPleosCredential).toHaveBeenCalledWith('hyundai', body)
  })

  it('tells the error, and drops it when the draft changes', async () => {
    api.setPleosCredential.mockRejectedValue(new Error('refused'))
    const { result } = withSetup(() => useSetPleosCredential('kia'))
    await result.setCredential(body).catch(() => {})
    expect(result.error.value).toEqual(new Error('refused'))
    result.reset()
    expect(result.error.value).toBeNull()
  })
})

describe('useDeletePleosCredential', () => {
  it('removes the key of the brand and refreshes the vehicles', async () => {
    api.deletePleosCredential.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(() => useDeletePleosCredential('genesis'))
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.deleteCredential()
    expect(api.deletePleosCredential).toHaveBeenCalledWith('genesis')
    expect(keys(invalidate)).toEqual(stale)
  })
})
