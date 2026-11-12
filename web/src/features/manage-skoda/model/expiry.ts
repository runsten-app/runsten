import type { SkodaKey } from '@/entities/connection'

// A MyŠkoda key expires: the API tells when on every response (expires_at), and the owner
// renews it in the app before. The reminder starts this long before, as a Polestar
// credential's.
export const renewAhead = 14 * 24 * 60 * 60_000

export type KeyState = 'valid' | 'soon' | 'expired' | 'refused'

// keyState tells where the key stands as of now: refused by Škoda, which an expiry past
// tells as expired; past its expiry; within the reminder; else valid. An expiry not told
// yet is valid.
export function keyState(k: SkodaKey, now: number): KeyState {
  const left = k.expires_at ? Date.parse(k.expires_at) - now : Infinity
  if (left <= 0) return 'expired'
  if (k.refused_at) return 'refused'
  return left <= renewAhead ? 'soon' : 'valid'
}

// renewalDue tells whether the key needs the user, from every page (the user menu's dot):
// refused, expired, or from the reminder's start on.
export function renewalDue(k: SkodaKey | null | undefined, now: number): boolean {
  return !!k && keyState(k, now) !== 'valid'
}
