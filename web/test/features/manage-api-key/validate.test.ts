import { describe, expect, it } from 'vitest'
import { keyLimits, validate } from '@/features/manage-api-key'

const key = '0123456789abcdef0123456789abcdef'

describe('validate', () => {
  it('takes a key as pasted, the spaces around it dropped', () => {
    expect(validate(`  ${key}\n`)).toEqual({ issues: [], key })
  })

  it.each([
    ['', 'required', undefined],
    ['   ', 'required', undefined],
    ['0123456789abcdef 0123456789abcdef', 'spaces', undefined],
    ['0123456789abcdéf0123456789abcdef', 'characters', undefined],
    ['0123456789', 'tooShort', { min: keyLimits.min }],
    ['a'.repeat(keyLimits.max + 1), 'tooLong', { max: keyLimits.max }],
  ])('refuses %j: %s', (draft, rule, params) => {
    const { issues, key } = validate(draft)
    expect(key).toBeNull()
    expect(issues).toEqual([{ field: 'api-key', rule, ...(params && { params }) }])
  })

  it('accepts the bounds', () => {
    expect(validate('a'.repeat(keyLimits.min)).key).not.toBeNull()
    expect(validate('a'.repeat(keyLimits.max)).key).not.toBeNull()
  })
})
