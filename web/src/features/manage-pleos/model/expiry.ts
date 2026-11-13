import type { PleosKey } from '@/entities/connection'

// A Pleos key reads for 90 days, as the Playground shows: the API tells no expiry, so its
// expires_at is an estimate, counted from when it was given. The reminder starts this
// long before, as a Polestar credential's.
export const renewAhead = 14 * 24 * 60 * 60_000

export type Expiry = 'valid' | 'soon' | 'past'

// expiry tells where a key stands as of now: valid, to renew soon, or past the date it
// should have expired by (it may still read: the date is an estimate).
export function expiry(expiresAt: string, now: number): Expiry {
  const left = Date.parse(expiresAt) - now
  if (left <= 0) return 'past'
  return left <= renewAhead ? 'soon' : 'valid'
}

// renewalDue tells whether the key needs the user, from every page (the user menu's dot):
// refused, or from the reminder's start on. Without one, nothing is due.
export function renewalDue(k: PleosKey | null | undefined, now: number): boolean {
  return !!k && (!!k.refused_at || expiry(k.expires_at, now) !== 'valid')
}
