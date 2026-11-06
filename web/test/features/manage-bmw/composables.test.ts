import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick } from 'vue'
import { connectionKeys } from '@/entities/connection'
import { vehicleKeys } from '@/entities/vehicle'
import { useBmwPolling, useDeleteBmw, useStartBmwAuthorization } from '@/features/manage-bmw'
import { ApiError } from '@/shared/api'
import connection from '@fixtures/connection-bmw.json'
import device from '@fixtures/bmw-authorization.json'
import { withSetup } from '@test/utils'

const api = vi.hoisted(() => ({
  startBmwAuthorization: vi.fn(),
  pollBmwToken: vi.fn(),
  deleteBmw: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

// The code of the golden file expires at 05:05, 5 minutes after the clock's start.
const start = Date.parse('2026-09-28T05:00:00Z')
beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  vi.useFakeTimers({ now: start })
})
afterEach(() => {
  vi.useRealTimers()
})

// What a connection changes: the connection, and each vehicle's state of reading.
const stale = [connectionKeys.current(), vehicleKeys.list(), vehicleKeys.details()]
const keys = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map(([f]) => (f as { queryKey?: unknown })?.queryKey)
const pending = (retryAfter?: number) => new ApiError(409, 'authorization_pending', '', retryAfter)

describe('useStartBmwAuthorization', () => {
  it("starts BMW's flow with the client ID, and keeps its code", async () => {
    api.startBmwAuthorization.mockResolvedValue(device)
    const { result } = withSetup(useStartBmwAuthorization)
    expect(await result.start('client-1234')).toEqual(device)
    expect(api.startBmwAuthorization).toHaveBeenCalledWith('client-1234')
    await vi.waitFor(() => expect(result.device.value).toEqual(device))
  })

  it('tells the error', async () => {
    api.startBmwAuthorization.mockRejectedValue(new ApiError(400, 'credential_refused', ''))
    const { result } = withSetup(useStartBmwAuthorization)
    await expect(result.start('client-1234')).rejects.toThrow()
    await vi.waitFor(() => expect(result.error.value).toBeInstanceOf(ApiError))
  })
})

describe('useDeleteBmw', () => {
  it('removes the connection and refreshes what it changes', async () => {
    api.deleteBmw.mockResolvedValue(undefined)
    const { result, queryClient } = withSetup(useDeleteBmw)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await result.remove()
    expect(api.deleteBmw).toHaveBeenCalled()
    expect(keys(invalidate)).toEqual(stale)
  })
})

describe('useBmwPolling', () => {
  it('asks once the interval passed, then as often as Retry-After says, until connected', async () => {
    api.pollBmwToken
      .mockRejectedValueOnce(pending(7))
      .mockRejectedValueOnce(pending())
      .mockResolvedValue(connection)
    const { result, queryClient } = withSetup(useBmwPolling)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    result.begin(device)
    expect(result.phase.value).toBe('waiting')
    // Not before BMW's interval (5 s).
    await vi.advanceTimersByTimeAsync(4_999)
    expect(api.pollBmwToken).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(1)
    // Retry-After 7 s wins over the interval.
    await vi.advanceTimersByTimeAsync(6_999)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(2)
    // No Retry-After: the interval.
    await vi.advanceTimersByTimeAsync(5_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(3)
    expect(result.phase.value).toBe('connected')
    expect(queryClient.getQueryData(connectionKeys.current())).toEqual(connection)
    expect(keys(invalidate)).toEqual(stale)
    // Done: no more request.
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(3)
  })

  it.each([
    ['authorization_expired', 400],
    ['credential_refused', 400],
    ['scope_missing', 409],
    ['no_vehicle', 409],
    ['too_many_vehicles', 409],
    ['not_admitted', 403],
    ['internal', 500],
  ] as const)('ends on %s, which it keeps', async (code, status) => {
    api.pollBmwToken.mockRejectedValue(new ApiError(status, code, ''))
    const { result } = withSetup(useBmwPolling)
    result.begin(device)
    await vi.advanceTimersByTimeAsync(5_000)
    expect(result.phase.value).toBe('failed')
    expect(result.error.value?.code).toBe(code)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(1)
  })

  it('keeps asking while BMW does not answer, and says so, then connects', async () => {
    api.pollBmwToken
      .mockRejectedValueOnce(new ApiError(502, 'source_unavailable', ''))
      .mockRejectedValueOnce(new ApiError(0, 'network', ''))
      .mockRejectedValueOnce(new ApiError(502, 'unavailable', ''))
      .mockRejectedValueOnce(pending())
      .mockResolvedValue(connection)
    const { result } = withSetup(useBmwPolling)
    result.begin(device)
    await vi.advanceTimersByTimeAsync(5_000)
    expect(result.unreachable.value).toBe(true)
    expect(result.phase.value).toBe('waiting')
    await vi.advanceTimersByTimeAsync(10_000)
    expect(result.unreachable.value).toBe(true)
    // BMW answers again: pending is a normal answer.
    await vi.advanceTimersByTimeAsync(5_000)
    expect(result.unreachable.value).toBe(false)
    await vi.advanceTimersByTimeAsync(5_000)
    expect(result.phase.value).toBe('connected')
  })

  it('ends with an expiry once the code expired, without asking BMW', async () => {
    api.pollBmwToken.mockRejectedValue(new ApiError(502, 'source_unavailable', ''))
    const { result } = withSetup(useBmwPolling)
    result.begin({ ...device, interval: 100 })
    // The second attempt is after the 05:05 expiry (the first, at 100 s, failed).
    await vi.advanceTimersByTimeAsync(300_000)
    expect(result.phase.value).toBe('failed')
    expect(result.error.value?.code).toBe('authorization_expired')
    expect(api.pollBmwToken.mock.calls.length).toBeLessThanOrEqual(3)
  })

  it('stops asking on cancel, and its in-flight answer leaves the phase alone', async () => {
    let answer: (c: typeof connection) => void = () => undefined
    api.pollBmwToken.mockReturnValue(new Promise((resolve) => (answer = resolve)))
    const { result, queryClient } = withSetup(useBmwPolling)
    result.begin(device)
    await vi.advanceTimersByTimeAsync(5_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(1)
    result.cancel()
    expect(result.phase.value).toBe('idle')
    answer(connection)
    await vi.advanceTimersByTimeAsync(0)
    await nextTick()
    // The connection was stored: the cache knows it, the form does not move.
    expect(queryClient.getQueryData(connectionKeys.current())).toEqual(connection)
    expect(result.phase.value).toBe('idle')
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(1)
  })

  it('stops asking when its scope ends', async () => {
    api.pollBmwToken.mockRejectedValue(pending())
    const scope = effectScope()
    const { result } = withSetup(() => scope.run(useBmwPolling))
    result?.begin(device)
    await vi.advanceTimersByTimeAsync(5_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(1)
    scope.stop()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(1)
  })
})
