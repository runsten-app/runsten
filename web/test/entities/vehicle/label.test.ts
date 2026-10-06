import { describe, expect, it } from 'vitest'
import { vehicleLabel, vehicleLabels, type Vehicle } from '@/entities/vehicle'
import reauth from '@fixtures/vehicle-reauth-required.json'
import vehicles from '@fixtures/vehicles.json'

type Model = Vehicle['model']

const unknown: Model = {
  family: null,
  model_year: null,
  variant: null,
  variant_source: null,
  ac_max_kw: null,
}
const ex30: Model = { ...unknown, family: 'EX30', model_year: 2024 }

function car(id: string, vin: string, model: Model) {
  return { id, vin, model }
}

describe('vehicleLabel', () => {
  it.each<[string, Model, string]>([
    ['the variant and the year', (vehicles.items[0] as Vehicle).model, 'XC40 Recharge Twin · 2021'],
    ['the family and the year', ex30, 'EX30 · 2024'],
    ['the family alone', { ...ex30, model_year: null }, 'EX30'],
    ['the VIN before the details are read', reauth.model as Model, 'YV1SMLT0000DT0002'],
  ])('names %s', (_, model, want) => {
    expect(vehicleLabel({ vin: 'YV1SMLT0000DT0002', model })).toBe(want)
  })
})

describe('vehicleLabels', () => {
  it('keeps distinct labels as they are', () => {
    const labels = vehicleLabels(vehicles.items as Vehicle[])
    expect([...labels.values()]).toEqual([
      'XC40 Recharge Twin · 2021',
      'YV1SMLT0000DT0002',
      'EX30 Single Motor Extended Range · 2024', // chosen by the driver
    ])
  })

  it('ends two alike labels with their serial number', () => {
    const labels = vehicleLabels([
      car('a', 'YV1EL3AV0R2000001', ex30),
      car('b', 'YV1EL3AV0R2000742', ex30),
      car('c', 'YV1EL3AV0R2000999', { ...ex30, model_year: 2025 }),
    ])
    expect(Object.fromEntries(labels)).toEqual({
      a: 'EX30 · 2024 · 000001',
      b: 'EX30 · 2024 · 000742',
      c: 'EX30 · 2025',
    })
  })
})
