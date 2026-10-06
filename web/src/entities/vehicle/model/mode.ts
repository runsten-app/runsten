import type { State } from './Vehicle'

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
