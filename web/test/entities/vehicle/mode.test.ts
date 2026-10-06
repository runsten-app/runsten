import { describe, expect, it } from 'vitest'
import { vehicleMode, type State } from '@/entities/vehicle'
import state from '@fixtures/state.json'
import unknown from '@fixtures/state-unknown.json'

const reading = <T>(value: T) => ({
  value,
  reported_at: null,
  fetched_at: '2026-09-28T19:00:00Z',
  checked_at: '2026-09-28T19:00:00Z',
})

function with_(engine: 'running' | 'stopped' | null, status: 'charging' | 'idle' | null) {
  return {
    engine: engine && reading(engine),
    charging: { ...(state as State).charging, status: status && reading(status) },
  }
}

describe('vehicleMode', () => {
  it.each([
    [with_('stopped', 'charging'), 'charging'],
    [with_(null, 'charging'), 'charging'],
    [with_('running', 'idle'), 'driving'],
    [with_('stopped', 'idle'), 'parked'],
    [with_('stopped', null), 'parked'],
    [with_(null, 'idle'), null],
  ])('%# → %s', (s, want) => expect(vehicleMode(s as State)).toBe(want))

  it('reads the fixtures', () => {
    expect(vehicleMode(state as State)).toBe('charging')
    expect(vehicleMode(unknown as State)).toBeNull()
  })
})
