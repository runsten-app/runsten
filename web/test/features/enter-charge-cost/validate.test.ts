import { describe, expect, it } from 'vitest'
import type { Cost } from '@/entities/charge'
import { draftOf, field, validate } from '@/features/enter-charge-cost/model/validate'
import entered from '@fixtures/charge-cost.json'
import charge from '@fixtures/charge.json'
import settings from '@fixtures/settings.json'

const limits = settings.limits.entered_cost

const draft = (d: Partial<{ amount: string; energy: string; note: string }>) => ({
  amount: '',
  energy: '',
  note: '',
  ...d,
})

describe('validate an entered cost', () => {
  it('builds the body, in minor units, a blank note as none', () => {
    expect(
      validate(draft({ amount: '12,34', energy: '31,5', note: '  Ionity  ' }), 2, limits),
    ).toEqual({
      issues: [],
      cost: { amount_minor: 1234, energy_kwh: 31.5, note: 'Ionity' },
    })
    expect(validate(draft({ amount: '0', note: '   ' }), 2, limits).cost).toEqual({
      amount_minor: 0,
      energy_kwh: null,
      note: null,
    })
    expect(validate(draft({ amount: '1240' }), 0, limits).cost?.amount_minor).toBe(1240)
  })

  it.each([
    [draft({}), field.amount, 'required', undefined],
    [draft({ amount: 'abc' }), field.amount, 'notNumber', undefined],
    [draft({ amount: '.5' }), field.amount, 'notNumber', undefined],
    [draft({ amount: '-1' }), field.amount, 'range', { min: 0, max: 10_000_000 }],
    [draft({ amount: '10000000.01' }), field.amount, 'range', { min: 0, max: 10_000_000 }],
    [draft({ amount: '1.234' }), field.amount, 'decimals', { max: 2 }],
    [draft({ amount: '1', energy: 'x' }), field.energy, 'notNumber', undefined],
    [draft({ amount: '1', energy: '0' }), field.energy, 'aboveZero', { max: 500 }],
    [draft({ amount: '1', energy: '500.1' }), field.energy, 'aboveZero', { max: 500 }],
    [draft({ amount: '1', note: 'x'.repeat(501) }), field.note, 'tooLong', { max: 500 }],
  ])('tells %o: %s %s', (d, f, rule, params) => {
    const { issues, cost } = validate(d, 2, limits)
    expect(issues).toEqual([{ field: f, rule, ...(params ? { params } : {}) }])
    expect(cost).toBeNull()
  })

  it('counts a note in characters, not UTF-16 units', () => {
    expect(validate(draft({ amount: '1', note: '🔌'.repeat(500) }), 2, limits).issues).toEqual([])
  })

  it('checks the limits it is given, not a copy of them', () => {
    const narrow = { amount_minor_max: 1000, energy_kwh_max: 50, note_chars: 5 }
    expect(
      validate(draft({ amount: '10.01', energy: '51', note: 'Ionity' }), 2, narrow).issues,
    ).toEqual([
      { field: field.amount, rule: 'range', params: { min: 0, max: 10 } },
      { field: field.energy, rule: 'aboveZero', params: { max: 50 } },
      { field: field.note, rule: 'tooLong', params: { max: 5 } },
    ])
  })

  it('refuses decimals in a currency without them', () => {
    expect(validate(draft({ amount: '12.5' }), 0, limits).issues).toEqual([
      { field: field.amount, rule: 'decimals', params: { max: 0 } },
    ])
  })
})

describe('draftOf', () => {
  it('starts from the entered cost, as a field writes it', () => {
    expect(draftOf(entered.cost as Cost, 2)).toEqual({
      amount: '8.50',
      energy: '20.1',
      note: 'Borne du parking, carte Chargemap',
    })
    expect(draftOf({ ...(entered.cost as Cost), energy_kwh: null, note: null }, 2)).toEqual({
      amount: '8.50',
      energy: '',
      note: '',
    })
  })

  // A tariff's estimate is not what was paid.
  it('starts empty from a tariff or no cost', () => {
    const empty = { amount: '', energy: '', note: '' }
    expect(draftOf(charge.cost as Cost, 2)).toEqual(empty)
    expect(draftOf(null, 2)).toEqual(empty)
  })
})
