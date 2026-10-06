import { describe, expect, it } from 'vitest'
import type { Model, VariantOption } from '@/entities/vehicle'
import {
  automatic,
  chargersOf,
  draftOf,
  effectiveOf,
  field,
  recognizedOf,
  validate,
  withVariant,
} from '@/features/edit-vehicle-model/model/draft'
import variants from '@fixtures/variants.json'
import chosen from '@fixtures/vehicle-model.json'
import vehicles from '@fixtures/vehicles.json'

// The EX30 of the fixtures: two candidates, the Single Motor Extended Range and the Twin
// Motor, then the rest of the family.
const options = variants.items as VariantOption[]
// The same, the details recognizing the Single Motor Extended Range.
const recognizing = options.map((o) => ({ ...o, candidate: o.id === 'ex30-er-2024' }))
const byId = (id: string) => options.find((o) => o.id === id) as VariantOption
const vehicle = chosen.id

describe('draftOf', () => {
  it.each<[string, Model, { variant: string; charger: string }]>([
    [
      'a chosen variant and charger',
      chosen.model as Model,
      { variant: 'ex30-er-2024', charger: '11' },
    ],
    [
      'a recognized variant: no choice',
      vehicles.items[0]?.model as Model,
      { variant: '', charger: '' },
    ],
    ['nothing known', vehicles.items[1]?.model as Model, { variant: '', charger: '' }],
  ])('starts from %s', (_, model, want) => {
    expect(draftOf(model)).toEqual(want)
  })
})

describe('the variant in effect', () => {
  it('is the only candidate, when there is one', () => {
    expect(recognizedOf(options)).toBeNull()
    expect(recognizedOf(recognizing)?.id).toBe('ex30-er-2024')
    expect(recognizedOf([])).toBeNull()
  })

  it('is the chosen one, else the recognized one', () => {
    expect(effectiveOf({ variant: 'ex30-p8-2027', charger: '' }, recognizing)?.id).toBe(
      'ex30-p8-2027',
    )
    expect(effectiveOf({ variant: automatic, charger: '' }, recognizing)?.id).toBe('ex30-er-2024')
    expect(effectiveOf({ variant: automatic, charger: '' }, options)).toBeNull()
    expect(effectiveOf({ variant: 'gone', charger: '' }, options)).toBeNull()
  })

  it('has its standard charger, and its option if any', () => {
    expect(chargersOf(byId('ex30-er-2024'))).toEqual([11, 22])
    expect(chargersOf(byId('ex30-lfp-2024'))).toEqual([11])
  })
})

describe('withVariant', () => {
  it('keeps a charger the new variant has, and forgets one it lacks', () => {
    const d = { variant: 'ex30-er-2024', charger: '22' }
    expect(withVariant(d, 'ex30-twin-2024', options)).toEqual({
      variant: 'ex30-twin-2024',
      charger: '22',
    })
    expect(withVariant(d, 'ex30-lfp-2024', options)).toEqual({
      variant: 'ex30-lfp-2024',
      charger: automatic,
    })
    expect(withVariant({ ...d, charger: '11' }, 'ex30-lfp-2024', options).charger).toBe('11')
    // Back to the recognition: its variant's chargers, or none without one.
    expect(withVariant(d, automatic, recognizing).charger).toBe('22')
    expect(withVariant(d, automatic, options).charger).toBe('')
  })
})

describe('validate', () => {
  it('builds the body of the choice', () => {
    expect(validate(vehicle, { variant: 'ex30-er-2024', charger: '11' }, options)).toEqual({
      issues: [],
      choice: { variant_id: 'ex30-er-2024', ac_max_kw: 11 },
    })
    expect(validate(vehicle, { variant: automatic, charger: automatic }, options).choice).toEqual({
      variant_id: null,
      ac_max_kw: null,
    })
    // The charger of the recognized variant, without choosing it.
    expect(validate(vehicle, { variant: automatic, charger: '22' }, recognizing).choice).toEqual({
      variant_id: null,
      ac_max_kw: 22,
    })
  })

  it('refuses what the API would', () => {
    expect(validate(vehicle, { variant: 'gone', charger: '' }, options).issues).toEqual([
      { field: field.variant(vehicle), rule: 'unknownVariant' },
    ])
    expect(validate(vehicle, { variant: 'ex30-lfp-2024', charger: '22' }, options)).toEqual({
      issues: [{ field: field.charger(vehicle), rule: 'notACharger' }],
      choice: null,
    })
    // No variant in effect: no charger.
    expect(validate(vehicle, { variant: automatic, charger: '11' }, options).issues).toEqual([
      { field: field.charger(vehicle), rule: 'notACharger' },
    ])
  })
})
