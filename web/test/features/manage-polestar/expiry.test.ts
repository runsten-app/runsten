import { describe, expect, it } from 'vitest'
import { expiry, renewAhead, renewalDue } from '@/features/manage-polestar'

const expires = '2026-12-27T05:00:00Z'
const at = Date.parse(expires)

describe('expiry', () => {
  it.each([
    [at - renewAhead - 1, 'valid'],
    [at - renewAhead, 'soon'],
    [at - 1, 'soon'],
    [at, 'past'],
    [at + 24 * 60 * 60_000, 'past'],
  ])('%d: %s', (now, want) => {
    expect(expiry(expires, now)).toBe(want)
  })

  it('reminds two weeks before', () => {
    expect(renewAhead).toBe(14 * 24 * 60 * 60_000)
  })
})

describe('renewalDue', () => {
  const credential = {
    last4: '0123',
    set_at: '2026-09-28T05:00:00Z',
    expires_at: expires,
    refused_at: null,
  }

  it.each([
    ['none', null, at, false],
    ['valid', credential, at - renewAhead - 1, false],
    ['to renew soon', credential, at - renewAhead, true],
    ['past its expiry', credential, at, true],
    ['refused', { ...credential, refused_at: '2026-10-01T05:00:00Z' }, at - renewAhead - 1, true],
  ])('%s', (_, c, now, want) => {
    expect(renewalDue(c, now)).toBe(want)
  })
})
