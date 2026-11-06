import { describe, expect, it } from 'vitest'
import { clientIdLimits, validate } from '@/features/manage-bmw'

describe('validate', () => {
  it('accepts a client ID, the spaces a copy adds around it dropped', () => {
    const id = '4c1cb0a0-1b0e-4d1e-9a3b-0f6f9c3d5e7a'
    expect(validate(`  ${id}\n`)).toEqual({ issue: null, clientId: id })
    expect(validate('a'.repeat(clientIdLimits.min)).issue).toBeNull()
    expect(validate('a'.repeat(clientIdLimits.max)).issue).toBeNull()
  })

  it.each([
    ['', { rule: 'required' }],
    ['   ', { rule: 'required' }],
    ['4c1cb0a0 1b0e4d1e', { rule: 'spaces' }],
    ['4c1cb0a0-é-1b0e4d1e', { rule: 'characters' }],
    ['abc', { rule: 'tooShort', params: { min: 8 } }],
    ['a'.repeat(129), { rule: 'tooLong', params: { max: 128 } }],
  ])('refuses %j', (draft, issue) => {
    expect(validate(draft)).toEqual({ issue, clientId: null })
  })
})
