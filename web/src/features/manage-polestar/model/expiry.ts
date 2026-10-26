import type { PolestarCredential } from '@/entities/connection'

// A Data Portal credential reads for 90 days (the API's expires_at), then the owner creates
// another. The reminder starts this long before.
export const renewAhead = 14 * 24 * 60 * 60_000

export type Expiry = 'valid' | 'soon' | 'past'

// expiry tells where a credential stands as of now: valid, to renew soon, or past the
// date it should have expired by (it may still read: the date is counted from when it was
// given, not from when it was created).
export function expiry(expiresAt: string, now: number): Expiry {
  const left = Date.parse(expiresAt) - now
  if (left <= 0) return 'past'
  return left <= renewAhead ? 'soon' : 'valid'
}

// renewalDue tells whether the credential needs the user, from every page (the user
// menu's dot): refused, or from the reminder's start on. Without one, nothing is due:
// the vehicles it would read tell it.
export function renewalDue(c: PolestarCredential | null | undefined, now: number): boolean {
  return !!c && (!!c.refused_at || expiry(c.expires_at, now) !== 'valid')
}
