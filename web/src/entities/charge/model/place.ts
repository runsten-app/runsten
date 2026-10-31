import type { Charge, ChargeInProgress } from './Charge'

// chargeAt tells whether a charge belongs to a group of the statistics' charges by place,
// as the API's place parameter names it: its place's ID, outside (a position, no place)
// or no_position (neither).
export function chargeAt(c: Charge | ChargeInProgress, place: string): boolean {
  if (place === 'outside') return !!c.position && !c.place
  if (place === 'no_position') return !c.position && !c.place
  return c.place?.id === place
}
