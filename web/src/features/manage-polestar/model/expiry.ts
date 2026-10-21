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
