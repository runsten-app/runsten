import { describe, expect, it } from 'vitest'
import { credentialLimits, validate } from '@/features/manage-polestar'

const ok = {
  client_id: 'runsten-dev',
  client_secret: 'runsten-dev-secret',
  account_id: 'acct-1234',
}

describe('validate', () => {
  it('builds the body, the spaces of a copy dropped around each value', () => {
    expect(
      validate({
        client_id: ' runsten-dev ',
        client_secret: 'runsten-dev-secret\n',
        account_id: 'acct-1234',
      }),
    ).toEqual({ issues: [], body: ok })
  })

  it.each([
    ['client_id', '', 'required', undefined],
    ['client_id', 'run sten', 'spaces', undefined],
    ['client_secret', 'sécret-1234', 'characters', undefined],
    ['account_id', 'abc', 'tooShort', { min: 4 }],
    ['client_id', 'a'.repeat(257), 'tooLong', { max: 256 }],
    ['client_secret', 'a'.repeat(513), 'tooLong', { max: 512 }],
  ] as const)('%s %j: %s', (field, value, rule, params) => {
    const checked = validate({ ...ok, [field]: value })
    expect(checked.body).toBeNull()
    expect(checked.issues).toEqual([{ field, rule, ...(params && { params }) }])
  })

  it('tells every value wrong, in the order of the form', () => {
    expect(
      validate({ client_id: '', client_secret: '', account_id: '' }).issues.map((i) => i.field),
    ).toEqual(['client_id', 'client_secret', 'account_id'])
  })

  it("repeats the API's bounds", () => {
    expect(credentialLimits).toEqual({
      client_id: { min: 4, max: 256 },
      client_secret: { min: 4, max: 512 },
      account_id: { min: 4, max: 256 },
    })
  })
})
