import type { Ford } from '@/entities/connection'

// Ford asks for a new sign-in a year after the owner's: the API tells when the grant ends
// (expires_at). The reminder starts this long before, as a Polestar credential's.
export const renewAhead = 14 * 24 * 60 * 60_000

export type GrantState = 'valid' | 'soon' | 'expired' | 'refused'

// grantState tells where the sign-in stands as of now: past its end; refused by Ford;
// within the reminder; else valid. An end not told is valid.
export function grantState(c: Ford, now: number): GrantState {
  const left = c.expires_at ? Date.parse(c.expires_at) - now : Infinity
  if (left <= 0) return 'expired'
  if (c.refused_at) return 'refused'
  return left <= renewAhead ? 'soon' : 'valid'
}

// renewalDue tells whether the sign-in needs the user, from every page (the user menu's
// dot): refused, over, or from the reminder's start on.
export function renewalDue(c: Ford | null | undefined, now: number): boolean {
  return !!c && grantState(c, now) !== 'valid'
}
