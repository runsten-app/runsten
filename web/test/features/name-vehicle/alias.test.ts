import { describe, expect, it } from 'vitest'
import { maxAlias, validate } from '@/features/name-vehicle/model/alias'

describe('validate', () => {
  it.each<[string, string, ReturnType<typeof validate>]>([
    ['an alias', 'Pool car 3', { body: { alias: 'Pool car 3' }, issue: null }],
    ['trimmed', '  Pool car 3 ', { body: { alias: 'Pool car 3' }, issue: null }],
    ['empty: removed', '', { body: { alias: null }, issue: null }],
    ['blank: removed', '   ', { body: { alias: null }, issue: null }],
    [
      'at the limit, by characters',
      'é'.repeat(maxAlias),
      { body: { alias: 'é'.repeat(maxAlias) }, issue: null },
    ],
    ['too long', 'a'.repeat(maxAlias + 1), { body: null, issue: 'tooLong' }],
    ['a tab', 'Pool\tcar', { body: null, issue: 'control' }],
  ])('%s', (_, text, want) => {
    expect(validate(text)).toEqual(want)
  })
})
