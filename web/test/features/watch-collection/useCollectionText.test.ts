import { describe, expect, it } from 'vitest'
import { useCollectionText } from '@/features/watch-collection'
import { withSetup } from '@test/utils'

describe('useCollectionText', () => {
  it("words a failure by the vehicle's vendor", () => {
    const { result: text } = withSetup(useCollectionText)
    expect(text.failure('unavailable')).toBe('Volvo unreachable')
    expect(text.failure('unavailable', 'polestar')).toBe('Polestar unreachable')
    expect(text.failure('token', 'polestar')).toBe('No token from Polestar')
    expect(text.failure('not_found', 'polestar')).toBe('Vehicle unknown to Polestar')
    expect(text.failure('credential_refused', 'polestar')).toBe('Polestar credential refused')
    expect(text.failure('unshared', 'polestar')).toBe('Vehicle no longer shared')
    expect(text.failure('not_granted', 'polestar')).toBe('Domain not granted')
    expect(text.failure('unavailable', 'tesla')).toBe('Tesla unreachable')
    expect(text.failure('token', 'tesla')).toBe('No token from Tesla')
    expect(text.failure('not_found', 'tesla')).toBe('Vehicle unknown to Tesla')
    expect(text.failure('credential_refused', 'tesla')).toBe('Tesla refresh token refused')
    expect(text.failure('api_closed', 'tesla')).toBe('API closed to the account')
    expect(text.failure('credential_refused', 'polestar_id')).toBe('Polestar ID refused')
    expect(text.failure('unavailable', 'polestar_id')).toBe('Polestar unreachable')
    expect(text.failure('credential_refused', 'tesla_fleet')).toBe('Tesla access withdrawn')
    expect(text.failure('unavailable', 'tesla_fleet')).toBe('Tesla unreachable')
  })

  it("words a failure and a state of a BMW by BMW's, and tells a stream held", () => {
    const { result: text } = withSetup(useCollectionText)
    expect(text.failure('stream_held', 'bmw')).toBe(
      'Stream held by another application (Home Assistant, evcc)',
    )
    // The same whatever the provider: only BMW's stream is held.
    expect(text.failure('stream_held')).toBe(text.failure('stream_held', 'bmw'))
    expect(text.failure('unavailable', 'bmw')).toBe('BMW unreachable')
    expect(text.failure('token', 'bmw')).toBe('No token from BMW')
    expect(text.failure('not_found', 'bmw')).toBe('Vehicle unknown to BMW')
    expect(text.failure('credential_refused', 'bmw')).toBe('BMW connection refused')
    const now = Date.parse('2026-09-28T07:05:00Z')
    expect(text.state({ kind: 'credential', refused: true }, now, 'bmw')).toBe(
      'Not read: BMW refused the connection',
    )
    expect(text.state({ kind: 'credential', refused: false }, now, 'bmw')).toBe(
      'Not read: no BMW connected',
    )
    expect(text.state({ kind: 'unshared' }, now, 'bmw')).toBe(
      "Not read: the BMW account is no longer the car's main user",
    )
    expect(text.state({ kind: 'paused', until: '2026-09-28T07:30:00Z' }, now, 'bmw')).toBe(
      'Paused by BMW until 07:30',
    )
    expect(
      text.state({ kind: 'quota', until: '2026-09-28T09:00:00Z', apis: ['bmw'] }, now, 'bmw'),
    ).toBe('Daily calls to BMW reached until 09:00')
  })

  it("names the Data Portal's API", () => {
    const { result: text } = withSetup(useCollectionText)
    expect(text.api('polestar')).toBe('Polestar Data Portal')
    expect(text.api('tesla')).toBe('Tesla Owner API')
    expect(text.api('polestar_id')).toBe('Polestar app API')
    expect(text.api('tesla_fleet')).toBe('Tesla Fleet API')
    expect(text.api('bmw')).toBe('BMW CarData')
    expect(text.api('elsewhere')).toBe('elsewhere')
  })
})
