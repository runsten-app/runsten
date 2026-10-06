import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchSession, sessionKeys, signIn, signOut } from '@/entities/session'
import session from '@fixtures/session.json'
import { json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

describe('session API', () => {
  it('reads, opens and closes the session', async () => {
    const requests = stubFetch({
      'GET /session': () => json(session),
      'POST /session': () => json(session),
      'DELETE /session': () => new Response(null, { status: 204 }),
    })
    expect(await fetchSession()).toEqual(session)
    expect(await signIn('admin', 'a long enough password')).toEqual(session)
    await signOut()
    const post = requests[1]
    if (!post) throw new Error('no POST')
    expect(post.headers.get('Content-Type')).toBe('application/json')
    expect(await post.json()).toEqual({ username: 'admin', password: 'a long enough password' })
    expect(requests.map((r) => r.method)).toEqual(['GET', 'POST', 'DELETE'])
  })

  it('has one key for the current session', () => {
    expect(sessionKeys.current()).toEqual(['session'])
  })
})
