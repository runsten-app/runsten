import { describe, expect, it } from 'vitest'
import { tokenLimits, validate } from '@/features/manage-tesla'

const token = 'eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.rt'

describe('validate', () => {
  it('keeps the token, the spaces of a copy dropped around it', () => {
    expect(validate(` ${token}\n`)).toEqual({ issue: null, token })
  })

  it.each([
    ['', 'required', undefined],
    ['eyJhbGciOi JSUzI1NiIsInR5c', 'spaces', undefined],
    ['eyJhbGciOiJSUzI1NiIsInR5cé', 'characters', undefined],
    ['eyJhbGciOi', 'tooShort', { min: tokenLimits.min }],
    ['a'.repeat(tokenLimits.max + 1), 'tooLong', { max: tokenLimits.max }],
  ] as const)('%j: %s', (value, rule, params) => {
    const checked = validate(value)
    expect(checked.token).toBeNull()
    expect(checked.issue).toEqual(params ? { rule, params } : { rule })
  })
})
