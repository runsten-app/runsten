import { describe, expect, it } from 'vitest'
import { keyState, renewalDue, validate, vinsOf } from '@/features/manage-skoda'

const vin = 'TMBJC9NY0RF000001'
const key = 'runsten-dev-skoda-key'

describe('validate', () => {
  it('builds the body: the key trimmed, the VINs in capitals, one a line or separated', () => {
    expect(
      validate({ api_key: `  ${key} `, vins: ` tmbjc9ny0rf000001,\nTMBNC7NY5SF083963 ` }),
    ).toEqual({
      issues: [],
      body: { api_key: key, vins: [vin, 'TMBNC7NY5SF083963'] },
    })
    expect(vinsOf('a; b  c')).toEqual(['A', 'B', 'C'])
  })

  it.each([
    [
      { api_key: '', vins: vin },
      { field: 'api_key', rule: 'required' },
    ],
    [
      { api_key: 'a b c d e f', vins: vin },
      { field: 'api_key', rule: 'spaces' },
    ],
    [
      { api_key: 'clé-à-coller', vins: vin },
      { field: 'api_key', rule: 'characters' },
    ],
    [
      { api_key: 'short', vins: vin },
      { field: 'api_key', rule: 'tooShort', params: { min: 8 } },
    ],
    [
      { api_key: 'k'.repeat(513), vins: vin },
      { field: 'api_key', rule: 'tooLong', params: { max: 512 } },
    ],
    [
      { api_key: key, vins: ' ' },
      { field: 'vins', rule: 'vinRequired' },
    ],
    [
      { api_key: key, vins: 'TMBJC9NY0RF00000O' },
      { field: 'vins', rule: 'vinInvalid', params: { vin: 'TMBJC9NY0RF00000O' } },
    ],
    [
      { api_key: key, vins: `${vin} ${vin}` },
      { field: 'vins', rule: 'vinTwice', params: { vin } },
    ],
    [
      { api_key: key, vins: Array.from({ length: 6 }, (_, i) => `TMBJC9NY0RF00000${i}`).join(' ') },
      { field: 'vins', rule: 'tooManyVins', params: { max: 5 } },
    ],
  ])('refuses %j', (draft, issue) => {
    expect(validate(draft)).toEqual({ issues: [issue], body: null })
  })
})

describe('keyState', () => {
  const now = Date.parse('2026-09-28T07:00:00Z')
  const k = (expires_at: string | null, refused_at: string | null = null) => ({
    last4: '-key',
    set_at: '2026-09-01T07:00:00Z',
    expires_at,
    refused_at,
  })

  it('tells where the key stands', () => {
    expect(keyState(k('2027-09-01T07:00:00Z'), now)).toBe('valid')
    expect(keyState(k(null), now)).toBe('valid')
    expect(keyState(k('2026-10-10T07:00:00Z'), now)).toBe('soon')
    expect(keyState(k('2026-09-28T06:00:00Z'), now)).toBe('expired')
    // Expired is the refusal's cause, told as such.
    expect(keyState(k('2026-09-28T06:00:00Z', '2026-09-28T06:30:00Z'), now)).toBe('expired')
    expect(keyState(k('2027-09-01T07:00:00Z', '2026-09-28T06:30:00Z'), now)).toBe('refused')
  })

  it('needs the user once refused, expired or within two weeks of its expiry', () => {
    expect(renewalDue(null, now)).toBe(false)
    expect(renewalDue(k('2027-09-01T07:00:00Z'), now)).toBe(false)
    expect(renewalDue(k('2026-10-10T07:00:00Z'), now)).toBe(true)
    expect(renewalDue(k(null, '2026-09-28T06:30:00Z'), now)).toBe(true)
  })
})
