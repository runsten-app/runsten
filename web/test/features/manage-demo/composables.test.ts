import { beforeEach, describe, expect, it, vi } from 'vitest'
import { connectionKeys, type ConnectionSettings } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'
import { useConnectDemo, useDisconnectDemo } from '@/features/manage-demo'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({ connectDemo: vi.fn(), deleteDemo: vi.fn() }))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})

// What the demo vehicle changes: the connection, and the vehicles.
const stale = [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()]
const keys = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)

describe('useConnectDemo', () => {
  it('connects, keeps the connection it gives back, and refreshes the vehicles', async () => {
    const connection = {
      volvo: true,
      demo: { set_at: '2026-09-28T05:00:00Z' },
    } as ConnectionSettings
    api.connectDemo.mockResolvedValue(connection)
    const { result, queryClient } = withSetup(() => useConnectDemo())
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(await result.connect()).toEqual(connection)
    expect(queryClient.getQueryData(connectionKeys.current())).toEqual(connection)
    expect(keys(invalidate)).toEqual(stale)
  })
})

describe('useDisconnectDemo', () => {
  it('deletes the vehicle and refreshes the vehicles', async () => {
    api.deleteDemo.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(() => useDisconnectDemo())
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.disconnect()
    expect(api.deleteDemo).toHaveBeenCalled()
    // Not awaited, but started at once.
    expect(keys(invalidate)).toEqual(stale)
  })
})
