import { describe, expect, it } from 'vitest'
import {
  collectionState,
  needsAttention,
  type CollectionState,
  type Vehicle,
} from '@/entities/vehicle'
import vehicles from '@fixtures/vehicles.json'

const [car, lost, never, unserved, polestar] = vehicles.items as Vehicle[]
const at = (iso: string) => Date.parse(iso)
const polestarConnection: Vehicle['connection'] = {
  provider: 'polestar',
  status: 'active',
  api_key: null,
  credential: 'set',
  authorized_at: null,
  refreshed_at: null,
  reauth_at: null,
  reauth_reason: null,
}
const notGranted = {
  at: '2026-09-28T07:04:00Z',
  endpoint: 'location',
  status: 403,
  kind: 'not_granted' as const,
}

describe('collectionState', () => {
  it('tells a Volvo unread on an instance without a Volvo application, before its grant', () => {
    const v = lost && {
      ...lost,
      connection: { ...lost.connection, status: 'not_configured' as const },
    }
    expect(v && collectionState(v, at('2026-09-28T07:05:00Z'))).toEqual({ kind: 'notConfigured' })
    expect(needsAttention({ kind: 'notConfigured' })).toBe(true)
  })

  it('reads a vehicle whose quota is exhausted until the latest of its APIs', () => {
    expect(car && collectionState(car, at('2026-09-28T07:05:00Z'))).toEqual({
      kind: 'quota',
      until: '2026-09-28T09:00:00Z',
      apis: ['energy', 'location'],
    })
    // Once the energy's ended, only the location's is left.
    const later = car?.collection && {
      ...car,
      collection: { ...car.collection, passed_at: '2026-09-28T08:04:00Z' },
    }
    expect(later && collectionState(later, at('2026-09-28T08:05:00Z'))).toEqual({
      kind: 'quota',
      until: '2026-09-28T09:00:00Z',
      apis: ['location'],
    })
  })

  it('tells the worst first', () => {
    const c = car?.collection
    if (!car || !c) throw new Error('fixture')
    const now = at('2026-09-28T07:05:00Z')
    const cases: [string, Vehicle, CollectionState][] = [
      [
        'the grant lost',
        { ...car, connection: lost?.connection ?? car.connection },
        { kind: 'reauth' },
      ],
      [
        'the key refused',
        { ...car, connection: { ...car.connection, api_key: 'refused' } },
        { kind: 'key', refused: true },
      ],
      [
        'no key at all',
        { ...car, connection: { ...car.connection, api_key: 'missing' } },
        { kind: 'key', refused: false },
      ],
      [
        'the market not served, before any pass',
        { ...car, market_refused_at: '2026-09-28T05:01:00Z', collection: null },
        { kind: 'market' },
      ],
      [
        'a Polestar credential refused',
        { ...car, connection: { ...polestarConnection, credential: 'refused' } },
        { kind: 'credential', refused: true },
      ],
      [
        'a Polestar credential removed, the vehicle unshared too',
        {
          ...car,
          connection: { ...polestarConnection, credential: 'missing' },
          unshared_at: '2026-09-28T06:00:00Z',
        },
        { kind: 'credential', refused: false },
      ],
      [
        'the vehicle no longer shared, before any pass',
        {
          ...car,
          connection: polestarConnection,
          unshared_at: '2026-09-28T06:00:00Z',
          collection: null,
        },
        { kind: 'unshared' },
      ],
      ['no pass yet', { ...car, collection: null }, { kind: 'waiting' }],
      [
        "the instance's key refused since the latest reading",
        {
          ...car,
          collection: {
            ...c,
            last_failure: {
              at: '2026-09-28T07:04:00Z',
              endpoint: 'odometer',
              status: 401,
              kind: 'key_refused',
            },
          },
        },
        { kind: 'key', refused: true },
      ],
      [
        "the instance's key refused, then read again",
        {
          ...car,
          collection: {
            ...c,
            quota: [],
            read_at: '2026-09-28T07:04:30Z',
            last_failure: {
              at: '2026-09-28T07:04:00Z',
              endpoint: 'odometer',
              status: 401,
              kind: 'key_refused',
            },
          },
        },
        { kind: 'reading' },
      ],
      [
        'its own key refused once: paused, tried again then',
        {
          ...car,
          connection: { ...car.connection, api_key: 'own' },
          collection: {
            ...c,
            paused_until: '2026-09-28T08:04:00Z',
            last_failure: {
              at: '2026-09-28T07:04:00Z',
              endpoint: 'odometer',
              status: 401,
              kind: 'key_refused',
            },
          },
        },
        { kind: 'paused', until: '2026-09-28T08:04:00Z' },
      ],
      [
        'the collector stopped',
        { ...car, collection: { ...c, passed_at: '2026-09-28T06:49:00Z' } },
        { kind: 'stopped', since: '2026-09-28T06:49:00Z' },
      ],
      [
        'a pause',
        { ...car, collection: { ...c, paused_until: '2026-09-28T07:30:00Z' } },
        { kind: 'paused', until: '2026-09-28T07:30:00Z' },
      ],
      [
        'a pause that ended, no quota',
        { ...car, collection: { ...c, paused_until: '2026-09-28T07:00:00Z', quota: [] } },
        { kind: 'reading' },
      ],
      [
        'a quota, a domain not granted',
        { ...car, connection: polestarConnection, collection: { ...c, last_failure: notGranted } },
        { kind: 'quota', until: '2026-09-28T09:00:00Z', apis: ['energy', 'location'] },
      ],
      [
        'a domain not granted, the others read',
        {
          ...car,
          connection: polestarConnection,
          collection: { ...c, quota: [], last_failure: notGranted },
        },
        { kind: 'notGranted', endpoint: 'location' },
      ],
    ]
    for (const [name, v, want] of cases) expect(collectionState(v, now), name).toEqual(want)
  })

  it('tells a vehicle the offer leaves unread before anything else', () => {
    const now = at('2026-09-28T07:05:00Z')
    expect(lost && collectionState(lost, now, true)).toEqual({ kind: 'unread' })
    expect(never && collectionState(never, now, true)).toEqual({ kind: 'unread' })
  })

  it('tells a domain not granted for a day, while the collector leaves it out', () => {
    const c = car?.collection
    if (!car || !c) throw new Error('fixture')
    const v = {
      ...car,
      connection: polestarConnection,
      collection: { ...c, quota: [], last_failure: notGranted },
    }
    const passed = (iso: string) => ({ ...v, collection: { ...v.collection, passed_at: iso } })
    expect(collectionState(passed('2026-09-29T07:00:00Z'), at('2026-09-29T07:03:59Z')).kind).toBe(
      'notGranted',
    )
    expect(collectionState(passed('2026-09-29T07:04:00Z'), at('2026-09-29T07:04:00Z'))).toEqual({
      kind: 'reading',
    })
  })

  it('takes a pass of 15 minutes ago for a running collector', () => {
    const c = car?.collection
    if (!car || !c) throw new Error('fixture')
    const v = { ...car, collection: { ...c, quota: [] } }
    expect(collectionState(v, at('2026-09-28T07:15:00Z'))).toEqual({ kind: 'reading' })
    expect(collectionState(v, at('2026-09-28T07:15:00.001Z')).kind).toBe('stopped')
  })

  it('reads the fixtures', () => {
    const now = at('2026-09-28T07:05:00Z')
    expect(lost && collectionState(lost, now)).toEqual({ kind: 'reauth' })
    expect(never && collectionState(never, now)).toEqual({ kind: 'waiting' })
    expect(unserved && collectionState(unserved, now)).toEqual({ kind: 'market' })
    expect(polestar && collectionState(polestar, now)).toEqual({ kind: 'unshared' })
  })
})

describe('needsAttention', () => {
  it.each<[CollectionState, boolean]>([
    [{ kind: 'unread' }, true],
    [{ kind: 'reauth' }, true],
    [{ kind: 'waiting' }, false],
    [{ kind: 'stopped', since: '' }, true],
    [{ kind: 'market' }, true],
    [{ kind: 'credential', refused: true }, true],
    [{ kind: 'unshared' }, true],
    [{ kind: 'notGranted', endpoint: null }, true],
    [{ kind: 'paused', until: '' }, true],
    [{ kind: 'quota', until: '', apis: [] }, true],
    [{ kind: 'reading' }, false],
  ])('%o: %s', (s, want) => {
    expect(needsAttention(s)).toBe(want)
  })
})
