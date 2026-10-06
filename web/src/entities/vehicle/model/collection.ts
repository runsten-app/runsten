import type { Vehicle } from './Vehicle'

export type Collection = NonNullable<Vehicle['collection']>
export type CollectionFailure = NonNullable<Collection['last_failure']>
export type FailureKind = CollectionFailure['kind']

// The collector writes its status again at least every minute while it runs, and its
// health check fails after 15 minutes without a pass: the same delay here, so that a
// long pass over many vehicles is not taken for a stop.
export const collectorStoppedAfter = 15 * 60_000

// CollectionState is what the user is told of the reading of a vehicle, the worst first:
// the account's offer leaves it unread, the grant lost, no application key it may be
// read with, no pass yet, the collector stopped, the instance's key refused, a pause, a
// quota; else it reads.
export type CollectionState =
  | { kind: 'unread' }
  | { kind: 'reauth' }
  | { kind: 'key'; refused: boolean }
  | { kind: 'waiting' }
  | { kind: 'stopped'; since: string }
  | { kind: 'paused'; until: string }
  | { kind: 'quota'; until: string; apis: string[] }
  | { kind: 'reading' }

// collectionState reads the status as of now: a pause or a quota the collector wrote may
// have ended since its latest pass. The quota's end is the latest of the blocked APIs',
// when everything is read again. unread: the account's limits leave the vehicle out
// (the session's limits.unread_vehicles).
export function collectionState(
  v: Pick<Vehicle, 'connection' | 'collection'>,
  now: number,
  unread = false,
): CollectionState {
  if (unread) return { kind: 'unread' }
  if (v.connection.status === 'reauth_required') return { kind: 'reauth' }
  if (v.connection.api_key === 'refused') return { kind: 'key', refused: true }
  if (v.connection.api_key === 'missing') return { kind: 'key', refused: false }
  const c = v.collection
  if (!c) return { kind: 'waiting' }
  if (now - Date.parse(c.passed_at) > collectorStoppedAfter) {
    return { kind: 'stopped', since: c.passed_at }
  }
  // The instance's key refused is the collector's only: it tries it again later, and
  // reads again once the administrator replaced it.
  const f = c.last_failure
  if (f?.kind === 'key_refused' && (!c.read_at || Date.parse(f.at) > Date.parse(c.read_at))) {
    return { kind: 'key', refused: true }
  }
  if (c.paused_until && Date.parse(c.paused_until) > now) {
    return { kind: 'paused', until: c.paused_until }
  }
  const blocked = c.quota.filter((q) => Date.parse(q.until) > now)
  if (blocked.length) {
    const until = blocked.reduce((a, b) => (Date.parse(b.until) > Date.parse(a.until) ? b : a))
    return { kind: 'quota', until: until.until, apis: blocked.map((q) => q.api) }
  }
  return { kind: 'reading' }
}

// needsAttention tells the states the user is warned of on every page of the vehicle:
// nothing new will be read until they end, or until the user acts.
export function needsAttention(s: CollectionState): boolean {
  return s.kind !== 'reading' && s.kind !== 'waiting'
}
