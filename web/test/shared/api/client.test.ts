import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, isApiError, onUnauthorized, unwrap } from '@/shared/api'
import session from '@fixtures/session.json'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

describe('unwrap', () => {
  it('returns the data', async () => {
    stubFetch({ 'GET /session': () => json(session) })
    expect(await unwrap(api.GET('/session'))).toEqual(session)
  })

  it.each([
    {
      res: () => apiError(401, 'invalid_credentials'),
      status: 401,
      code: 'invalid_credentials',
      retry: undefined,
    },
    {
      res: () => apiError(429, 'too_many_attempts', { 'Retry-After': '120' }),
      status: 429,
      code: 'too_many_attempts',
      retry: 120,
    },
    {
      res: () => apiError(429, 'too_many_attempts', { 'Retry-After': 'soon' }),
      status: 429,
      code: 'too_many_attempts',
      retry: undefined,
    },
    {
      res: () => new Response('bad gateway', { status: 502 }),
      status: 502,
      code: 'unavailable',
      retry: undefined,
    },
    {
      res: () => new Response('oops', { status: 500 }),
      status: 500,
      code: 'internal',
      retry: undefined,
    },
  ])('throws $code for $status', async ({ res, status, code, retry }) => {
    stubFetch({ 'POST /session': res })
    const e = await unwrap(api.POST('/session', { body: { username: 'a', password: 'b' } })).catch(
      (e: unknown) => e,
    )
    expect(e).toBeInstanceOf(ApiError)
    expect(e).toMatchObject({ status, code, retryAfter: retry })
    expect(isApiError(e, code as ApiError['code'])).toBe(true)
  })

  it('throws a network error when the request fails', async () => {
    vi.stubGlobal('fetch', () => Promise.reject(new TypeError('Failed to fetch')))
    await expect(unwrap(api.GET('/session'))).rejects.toMatchObject({ status: 0, code: 'network' })
  })
})

describe('onUnauthorized', () => {
  it('is told of a 401, except from /session', async () => {
    const told = vi.fn()
    const stop = onUnauthorized(told)
    stubFetch({
      'GET /vehicles': () => apiError(401, 'unauthorized'),
      'GET /session': () => apiError(401, 'unauthorized'),
      'POST /session': () => apiError(401, 'invalid_credentials'),
    })
    await unwrap(api.POST('/session', { body: { username: 'a', password: 'b' } })).catch(() => {})
    await unwrap(api.GET('/session')).catch(() => {})
    expect(told).not.toHaveBeenCalled()
    await unwrap(api.GET('/vehicles')).catch(() => {})
    expect(told).toHaveBeenCalledOnce()
    stop()
    await unwrap(api.GET('/vehicles')).catch(() => {})
    expect(told).toHaveBeenCalledOnce()
  })
})
