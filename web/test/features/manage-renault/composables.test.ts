import { beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionKeys, type ConnectionSettings } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'
import { useDeleteRenault, useRenaultCode, useSignInRenault } from '@/features/manage-renault'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({
  signInRenault: vi.fn(),
  setRenaultCode: vi.fn(),
  deleteRenault: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

// What a sign-in changes: the connection, and each vehicle's state of reading.
const stale = [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()]
const keys = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)
const connection = { volvo: true, renault: { last4: '1a2b' } } as unknown as ConnectionSettings
const body = { email: 'me@example.com', password: 'secret', language: 'fr' }

describe('useSignInRenault', () => {
  it('signs in, keeps the connection it gives back, and refreshes the vehicles', async () => {
    const answer = { code_sent_to: null, connection, others: 1 }
    api.signInRenault.mockResolvedValue(answer)
    const { result, queryClient } = withSetup(() => useSignInRenault())
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(await result.signIn(body)).toEqual(answer)
    expect(api.signInRenault).toHaveBeenCalledWith(body, expect.anything())
    expect(queryClient.getQueryData(connectionKeys.current())).toEqual(connection)
    expect(keys(invalidate)).toEqual(stale)
  })

  it('stores nothing while the code is awaited', async () => {
    api.signInRenault.mockResolvedValue({
      code_sent_to: 'e***@example.com',
      connection: null,
      others: 0,
    })
    const { result, queryClient } = withSetup(() => useSignInRenault())
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.signIn(body)
    expect(queryClient.getQueryData(connectionKeys.current())).toBeUndefined()
    expect(invalidate).not.toHaveBeenCalled()
  })
})

describe('useRenaultCode', () => {
  it('enters the code, keeps the connection, and refreshes the vehicles', async () => {
    const answer = { code_sent_to: null, connection, others: 1 }
    api.setRenaultCode.mockResolvedValue(answer)
    const { result, queryClient } = withSetup(() => useRenaultCode())
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(await result.enterCode('123456')).toEqual(answer)
    expect(api.setRenaultCode).toHaveBeenCalledWith('123456', expect.anything())
    expect(queryClient.getQueryData(connectionKeys.current())).toEqual(connection)
    expect(keys(invalidate)).toEqual(stale)
  })
})

describe('useDeleteRenault', () => {
  it('removes the sign-in and refreshes the vehicles', async () => {
    api.deleteRenault.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(() => useDeleteRenault())
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.remove()
    expect(api.deleteRenault).toHaveBeenCalled()
    expect(keys(invalidate)).toEqual(stale)
  })
})
