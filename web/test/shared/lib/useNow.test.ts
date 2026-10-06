import { afterEach, describe, expect, it, vi } from 'vitest'
import { useNow } from '@/shared/lib'
import { withSetup } from '@test/utils'

afterEach(() => {
  vi.useRealTimers()
})

describe('useNow', () => {
  it('moves on every interval, and stops with its component', () => {
    vi.useFakeTimers({ now: Date.parse('2026-09-28T19:00:00Z') })
    const { result: now, wrapper } = withSetup(() => useNow(30_000))
    expect(now.value).toBe(Date.parse('2026-09-28T19:00:00Z'))
    vi.advanceTimersByTime(29_999)
    expect(now.value).toBe(Date.parse('2026-09-28T19:00:00Z'))
    vi.advanceTimersByTime(1)
    expect(now.value).toBe(Date.parse('2026-09-28T19:00:30Z'))
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
