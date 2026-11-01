import { describe, expect, it } from 'vitest'
import { limits, validateCallback, validatePassword } from '@/features/manage-polestar-id'

const callback = 'https://www.polestar.com/sign-in-callback?code=abc.def&state=XYZ'

describe('validateCallback', () => {
  it('takes the address of the sign-in-callback, or its code alone, trimmed', () => {
    expect(validateCallback(` ${callback} `, true)).toEqual({ issues: [], callback })
    expect(validateCallback('abc.def', true)).toEqual({ issues: [], callback: 'abc.def' })
  })

  it.each([
    ['', 'required'],
    ['https://www.polestar.com/uk/auth/error', 'noCode'],
    ['https://www.polestar.com/sign-in-callback?code=', 'noCode'],
    ['not an address', 'noCode'],
    ['x'.repeat(limits.callback + 1), 'tooLong'],
  ])('refuses %j', (draft, rule) => {
    const { issues, callback: value } = validateCallback(draft, true)
    expect(value).toBeNull()
    expect(issues.map((i) => i.rule)).toEqual([rule])
  })

  it('asks for a sign-in started first', () => {
    const { issues, callback: value } = validateCallback(callback, false)
    expect(value).toBeNull()
    expect(issues).toEqual([{ field: 'polestar-id-start', rule: 'start' }])
  })
})

describe('validatePassword', () => {
  it('takes the email trimmed, the password as typed', () => {
    expect(validatePassword(' owner@example.com ', ' secret ')).toEqual({
      issues: [],
      body: { email: 'owner@example.com', password: ' secret ' },
    })
  })

  it('tells each field wrong', () => {
    expect(validatePassword('', '').issues.map((i) => [i.field, i.rule])).toEqual([
      ['polestar-id-email', 'required'],
      ['polestar-id-password', 'required'],
    ])
    expect(validatePassword('owner', 'x').issues.map((i) => i.rule)).toEqual(['email'])
    expect(
      validatePassword(`${'a'.repeat(limits.email)}@x`, 'p'.repeat(limits.password + 1)).issues.map(
        (i) => i.rule,
      ),
    ).toEqual(['tooLong', 'tooLong'])
  })
})
