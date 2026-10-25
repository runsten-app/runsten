import type { Reading, State } from './Vehicle'

export type Mode = 'parked' | 'driving' | 'charging'

// vehicleMode is a presentation of the state, as the collector derives its own polling
// mode: charging while charging, else driving while the engine runs, else parked. The API
// exposes no mode. null when neither is known.
export function vehicleMode(s: Pick<State, 'engine' | 'charging'>): Mode | null {
  if (s.charging.status?.value === 'charging') return 'charging'
  if (s.engine?.value === 'running') return 'driving'
  if (s.engine?.value === 'stopped') return 'parked'
  return null
}

// noChargeReading is the car's own word that no charge runs: its charging status idle,
// done or scheduled, else, without a status, its charge type none. A power missing then
// has nothing to measure (an EX30 answers it PROPERTY_NOT_FOUND while idle), where one
// missing during a charge stays unknown. null when the car says no such thing, or charges
// or discharges.
export function noChargeReading(c: State['charging']): Reading | null {
  const status = c.status?.value
  if (status === 'idle' || status === 'done' || status === 'scheduled') return c.status
  if (!c.status && c.type?.value === 'none') return c.type
  return null
}
