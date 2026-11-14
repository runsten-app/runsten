import type { Renault } from '@/entities/connection'

// renewalDue tells whether the sign-in needs the user, from every page (the user menu's
// dot): Renault refused it since, so its vehicles are not read until the owner signs in
// again. The session has no expiry the API tells.
export function renewalDue(r: Renault | null | undefined): boolean {
  return !!r?.refused_at
}
