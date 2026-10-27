import { flushPromises } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { sessionKeys, type Session } from '@/entities/session'
import { useFlag } from '@/features/view-flags'
import session from '@fixtures/session.json'
import { testQueryClient, withSetup } from '@test/utils'

function flag(s: Session | undefined) {
  const queryClient = testQueryClient()
  if (s) {
    queryClient.setQueryData(sessionKeys.current(), s)
  }
  return withSetup(() => useFlag('source.tesla'), { queryClient })
}

describe('useFlag', () => {
  it("reads the flag the API evaluated for the session's account", async () => {
    const on = flag(session as Session)
    await flushPromises()
    expect(on.result.value).toBe(true)

    const off = flag({ ...(session as Session), flags: { 'source.tesla': false } })
    await flushPromises()
    expect(off.result.value).toBe(false)
  })

  it('is off until the session is known', () => {
    expect(flag(undefined).result.value).toBe(false)
  })
})
