import { describe, expect, it } from 'vitest'
import { renewalDue, validateCode, validateSignIn } from '@/features/manage-renault'

describe('validateSignIn', () => {
  it('builds the body: the email trimmed, the password as typed', () => {
    expect(validateSignIn('  me@example.com ', ' pass word ')).toEqual({
      issues: [],
      body: { email: 'me@example.com', password: ' pass word ' },
    })
  })

  it.each([
    ['', 'x', [{ field: 'renault-email', rule: 'required' }]],
    ['not an email', 'x', [{ field: 'renault-email', rule: 'email' }]],
    [
      `${'a'.repeat(260)}@b.c`,
      'x',
      [{ field: 'renault-email', rule: 'tooLong', params: { max: 254 } }],
    ],
    ['me@example.com', '', [{ field: 'renault-password', rule: 'required' }]],
    [
      'me@example.com',
      'p'.repeat(257),
      [{ field: 'renault-password', rule: 'tooLong', params: { max: 256 } }],
    ],
  ])('refuses %j, %j', (email, password, issues) => {
    expect(validateSignIn(email, password)).toEqual({ issues, body: null })
  })

  it('tells both values at once', () => {
    expect(validateSignIn('', '').issues.map((i) => i.field)).toEqual([
      'renault-email',
      'renault-password',
    ])
  })
})

describe('validateCode', () => {
  it.each([
    ['123456', '123456'],
    [' 1234 ', '1234'],
    ['123 456', '123456'],
    ['12345678', '12345678'],
  ])('reads %j as %j', (draft, code) => {
    expect(validateCode(draft)).toEqual({ issues: [], code })
  })

  it.each([
    ['', 'required'],
    ['123', 'codeFormat'],
    ['123456789', 'codeFormat'],
    ['12a456', 'codeFormat'],
  ])('refuses %j', (draft, rule) => {
    expect(validateCode(draft)).toEqual({ issues: [{ field: 'renault-code', rule }], code: null })
  })
})

describe('renewalDue', () => {
  const set = { last4: '1a2b', set_at: '2026-11-01T07:00:00Z' }
  it('is due once Renault refused the sign-in', () => {
    expect(renewalDue({ ...set, refused_at: '2026-11-02T07:00:00Z' })).toBe(true)
    expect(renewalDue({ ...set, refused_at: null })).toBe(false)
    expect(renewalDue(null)).toBe(false)
    expect(renewalDue(undefined)).toBe(false)
  })
})
