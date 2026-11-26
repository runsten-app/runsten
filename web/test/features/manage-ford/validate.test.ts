import { describe, expect, it } from 'vitest'
import type { Ford } from '@/entities/connection'
import {
  grantState,
  renewAhead,
  renewalDue,
  validateApplication,
  validateCallback,
} from '@/features/manage-ford'

describe('validateApplication', () => {
  it('takes a client ID and a secret, the spaces a copy adds dropped', () => {
    expect(validateApplication(' 0a1b2c3d-client \n', '\tS3cr3t~value ')).toEqual({
      issues: [],
      app: { clientId: '0a1b2c3d-client', clientSecret: 'S3cr3t~value' },
    })
  })

  it('tells each field what is wrong with it', () => {
    const rules = (id: string, secret: string) =>
      validateApplication(id, secret).issues.map((i) => [i.field, i.rule, i.params])
    expect(rules('', '')).toEqual([
      ['ford-client-id', 'required', undefined],
      ['ford-client-secret', 'required', undefined],
    ])
    expect(rules('short', 'a secret with spaces')).toEqual([
      ['ford-client-id', 'tooShort', { min: 8 }],
      ['ford-client-secret', 'characters', undefined],
    ])
    expect(rules('c'.repeat(129), 's'.repeat(513))).toEqual([
      ['ford-client-id', 'tooLong', { max: 128 }],
      ['ford-client-secret', 'tooLong', { max: 512 }],
    ])
    expect(validateApplication('short', 'long-enough').app).toBeNull()
  })
})

describe('validateCallback', () => {
  it('takes the address Ford sent to, or its code alone', () => {
    const address = 'http://localhost/auth/ford/callback?code=abc123&state=XYZ'
    expect(validateCallback(`  ${address} `)).toEqual({ issues: [], callback: address })
    expect(validateCallback('abc-123_DEF').callback).toBe('abc-123_DEF')
  })

  it('refuses nothing, an address without a code, and what is neither', () => {
    const rule = (s: string) => validateCallback(s).issues.map((i) => i.rule)
    expect(rule('   ')).toEqual(['required'])
    expect(rule('http://localhost/auth/ford/callback?state=XYZ')).toEqual(['noCode'])
    expect(rule('code=abc&state=XYZ')).toEqual(['noCode'])
    expect(rule('two words')).toEqual(['noCode'])
    expect(rule(`http://localhost/?code=${'c'.repeat(8192)}`)).toEqual(['tooLong'])
  })
})

describe('grantState', () => {
  const now = Date.parse('2026-11-25T12:00:00Z')
  const ford = (over: Partial<Ford> = {}): Ford => ({
    last4: 'abcd',
    set_at: '2026-01-01T00:00:00Z',
    expires_at: '2027-01-01T00:00:00Z',
    refused_at: null,
    ...over,
  })

  it('is valid until two weeks before the year ends, then due, then over', () => {
    expect(grantState(ford(), now)).toBe('valid')
    expect(grantState(ford({ expires_at: null }), now)).toBe('valid')
    const end = (ms: number) => ford({ expires_at: new Date(now + ms).toISOString() })
    expect(grantState(end(renewAhead + 60_000), now)).toBe('valid')
    expect(grantState(end(renewAhead), now)).toBe('soon')
    expect(grantState(end(0), now)).toBe('expired')
  })

  it('is refused once Ford refused it, over when its year ended too', () => {
    const refused_at = '2026-11-20T00:00:00Z'
    expect(grantState(ford({ refused_at }), now)).toBe('refused')
    expect(grantState(ford({ refused_at, expires_at: '2026-11-19T00:00:00Z' }), now)).toBe(
      'expired',
    )
  })

  it('needs the user unless valid, and never without a connection', () => {
    expect(renewalDue(ford(), now)).toBe(false)
    expect(renewalDue(ford({ refused_at: '2026-11-20T00:00:00Z' }), now)).toBe(true)
    expect(renewalDue(null, now)).toBe(false)
    expect(renewalDue(undefined, now)).toBe(false)
  })
})
