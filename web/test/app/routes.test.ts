import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { createQueryClient } from '@/app/providers/query'
import { createAppRouter } from '@/app/routes'
import type { ExtensionGuard } from '@/app/routes/guard'
import { ApiError } from '@/shared/api'
import session from '@fixtures/session.json'

const { fetchSession } = vi.hoisted(() => ({ fetchSession: vi.fn() }))
vi.mock('@/entities/session', async (original) => ({
  ...(await original<typeof import('@/entities/session')>()),
  fetchSession,
}))

// The extensions' guards: none in the public build, set by a test.
const guards = vi.hoisted((): ExtensionGuard[] => [])
vi.mock('@/app/routes/extensions', () => ({ extensionRoutes: [], extensionGuards: guards }))

// Braces: a function returned by beforeEach is taken for a teardown, and the mock called.
beforeEach(() => {
  fetchSession.mockReset()
  guards.length = 0
})

function router() {
  return createAppRouter(createMemoryHistory(), createQueryClient())
}

describe('session guard', () => {
  it('lets a signed-in user in, and asks the session once', async () => {
    fetchSession.mockResolvedValue(session)
    const r = router()
    await r.push('/')
    await r.push('/login')
    await r.push('/')
    expect(r.currentRoute.value.name).toBe('home')
    expect(fetchSession).toHaveBeenCalledOnce()
  })

  it('sends to the sign-in, then back', async () => {
    fetchSession.mockRejectedValue(new ApiError(401, 'unauthorized', ''))
    const r = router()
    await r.push('/somewhere?x=1')
    expect(r.currentRoute.value.name).toBe('login')
    // An unknown path leads home, with its query.
    expect(r.currentRoute.value.query.next).toBe('/?x=1')
  })

  it('keeps the page asked for', async () => {
    fetchSession.mockRejectedValue(new ApiError(401, 'unauthorized', ''))
    const r = router()
    await r.push('/')
    expect(r.currentRoute.value.fullPath).toBe('/login?next=/')
  })

  it('does not hide other failures', async () => {
    fetchSession.mockRejectedValue(new ApiError(404, 'not_found', ''))
    const r = router()
    await expect(r.push('/')).rejects.toMatchObject({ code: 'not_found' })
  })
})

describe("extensions' guards", () => {
  it('run in order once the session is loaded, the first that sends elsewhere wins', async () => {
    fetchSession.mockResolvedValue(session)
    const seen: string[] = []
    const first = vi.fn<ExtensionGuard>(async (to) => {
      seen.push(`first ${to.fullPath}`)
      return to.name === 'settings' ? true : { name: 'settings', query: { next: to.fullPath } }
    })
    const second = vi.fn<ExtensionGuard>(async (to) => {
      seen.push(`second ${to.fullPath}`)
      return true
    })
    guards.push(first, second)
    const r = router()
    await r.push('/vehicles/v1')
    expect(r.currentRoute.value.fullPath).toBe('/settings?next=/vehicles/v1')
    expect(seen).toEqual([
      'first /vehicles/v1',
      'first /settings?next=/vehicles/v1',
      'second /settings?next=/vehicles/v1',
    ])
  })

  it('never run without a session, nor on a public page', async () => {
    fetchSession.mockRejectedValue(new ApiError(401, 'unauthorized', ''))
    const guard = vi.fn<ExtensionGuard>(async () => true)
    guards.push(guard)
    const r = router()
    await r.push('/')
    expect(r.currentRoute.value.name).toBe('login')
    expect(guard).not.toHaveBeenCalled()
  })
})

describe('routes', () => {
  it('give the vehicle of the path to its page', async () => {
    fetchSession.mockResolvedValue(session)
    const r = router()
    await r.push('/vehicles/v1')
    expect(r.currentRoute.value.name).toBe('vehicle')
    expect(r.currentRoute.value.matched[0]?.props.default).toBe(true)
    expect(r.currentRoute.value.params.vehicle).toBe('v1')
  })

  it.each([
    ['trips', '/vehicles/v1/trips'],
    ['charges', '/vehicles/v1/charges'],
  ])('lead to the %s of a vehicle, with their period', async (name, path) => {
    fetchSession.mockResolvedValue(session)
    const r = router()
    await r.push(`${path}?from=2026-09-01&to=2026-09-30`)
    expect(r.currentRoute.value.name).toBe(name)
    expect(r.currentRoute.value.query).toEqual({ from: '2026-09-01', to: '2026-09-30' })
  })

  it.each(['trip', 'charge'])('carry the ID of a %s through the path, and back', async (name) => {
    fetchSession.mockResolvedValue(session)
    const r = router()
    const id = '2026-09-28T07:01:00.000000Z'
    await r.push({ name, params: { vehicle: 'v1', id } })
    expect(r.currentRoute.value.path).toBe(`/vehicles/v1/${name}s/${id}`)
    expect(r.currentRoute.value.params.id).toBe(id)
    expect(r.currentRoute.value.matched.at(-1)?.props.default).toBe(true)
    // A link encoded by hand (encodeURIComponent) reads the same.
    await r.push(`/vehicles/v1/${name}s/${encodeURIComponent(id)}`)
    expect(r.currentRoute.value.params.id).toBe(id)
    // An ID with a reserved character stays one segment.
    await r.push({ name, params: { vehicle: 'v1', id: 'a/b?c#d' } })
    expect(r.currentRoute.value.params.id).toBe('a/b?c#d')
  })
})

describe('query client', () => {
  it('retries once, but never a client error', () => {
    const retry = createQueryClient().getDefaultOptions().queries?.retry as (
      n: number,
      e: unknown,
    ) => boolean
    expect(retry(0, new ApiError(500, 'internal', ''))).toBe(true)
    expect(retry(0, new ApiError(0, 'network', ''))).toBe(true)
    expect(retry(1, new ApiError(500, 'internal', ''))).toBe(false)
    expect(retry(0, new ApiError(404, 'not_found', ''))).toBe(false)
  })
})

describe('statistics page', () => {
  it('is loaded on demand', async () => {
    fetchSession.mockResolvedValue(session)
    const r = router()
    await r.push('/vehicles/v1/stats')
    expect(r.currentRoute.value.name).toBe('stats')
    const record = r.currentRoute.value.matched[0]
    expect(typeof record?.components?.default).toBe('object')
    expect(record?.props.default).toBe(true)
  })
})

describe('battery page', () => {
  it('is loaded on demand, like the statistics: the charts library weighs there', async () => {
    fetchSession.mockResolvedValue(session)
    const r = router()
    await r.push('/vehicles/v1/battery')
    expect(r.currentRoute.value.name).toBe('battery')
    expect(r.currentRoute.value.params.vehicle).toBe('v1')
    const record = r.currentRoute.value.matched[0]
    expect(typeof record?.components?.default).toBe('object')
    expect(record?.props.default).toBe(true)
  })
})

describe('settings', () => {
  it.each([
    ['/settings', 'settings'],
    ['/preferences', 'preferences'],
    ['/vehicles/v1/settings', 'vehicleSettings'],
  ])('%s is loaded on demand', async (path, name) => {
    fetchSession.mockResolvedValue(session)
    const r = router()
    await r.push(path)
    expect(r.currentRoute.value.name).toBe(name)
    expect(typeof r.currentRoute.value.matched[0]?.components?.default).toBe('object')
  })

  it('open a new place, at a position with a point or a comma', async () => {
    fetchSession.mockResolvedValue(session)
    const r = router()
    await r.push('/settings/places/new?lat=45,764&lon=4.8357')
    expect(r.currentRoute.value.name).toBe('place')
    const props = r.currentRoute.value.matched[0]?.props.default as (to: unknown) => unknown
    expect(props(r.currentRoute.value)).toEqual({
      place: undefined,
      lat: 45.764,
      lon: 4.8357,
    })
    await r.push('/settings/places/new?lat=north&lon=')
    expect(props(r.currentRoute.value)).toEqual({
      place: undefined,
      lat: undefined,
      lon: undefined,
    })
  })

  it('open a place by its ID', async () => {
    fetchSession.mockResolvedValue(session)
    const r = router()
    await r.push({ name: 'place', params: { place: 'p1' } })
    expect(r.currentRoute.value.path).toBe('/settings/places/p1')
    const props = r.currentRoute.value.matched[0]?.props.default as (to: unknown) => unknown
    expect(props(r.currentRoute.value)).toMatchObject({ place: 'p1' })
  })
})
