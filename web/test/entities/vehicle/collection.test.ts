import { describe, expect, it } from 'vitest'
import {
  collectionState,
  needsAttention,
  type CollectionState,
  type Vehicle,
} from '@/entities/vehicle'
import vehicles from '@fixtures/vehicles.json'

const [car, lost, never] = vehicles.items as Vehicle[]
const at = (iso: string) => Date.parse(iso)

describe('collectionState', () => {
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
    ]
    for (const [name, v, want] of cases) expect(collectionState(v, now), name).toEqual(want)
  })

  it('tells a vehicle the offer leaves unread before anything else', () => {
    const now = at('2026-09-28T07:05:00Z')
    expect(lost && collectionState(lost, now, true)).toEqual({ kind: 'unread' })
    expect(never && collectionState(never, now, true)).toEqual({ kind: 'unread' })
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
  })
})

describe('needsAttention', () => {
  it.each<[CollectionState, boolean]>([
    [{ kind: 'unread' }, true],
    [{ kind: 'reauth' }, true],
    [{ kind: 'waiting' }, false],
    [{ kind: 'stopped', since: '' }, true],
    [{ kind: 'paused', until: '' }, true],
    [{ kind: 'quota', until: '', apis: [] }, true],
    [{ kind: 'reading' }, false],
  ])('%o: %s', (s, want) => {
    expect(needsAttention(s)).toBe(want)
  })
})
