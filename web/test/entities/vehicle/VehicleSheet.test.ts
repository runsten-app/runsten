import { describe, expect, it } from 'vitest'
import { nextTick, reactive } from 'vue'
import { VehiclePicture, VehicleSheet, type Sheet, type Vehicle } from '@/entities/vehicle'
import vehicles from '@fixtures/vehicles.json'
import { mountWith } from '@test/utils'

// xc40 is a Volvo's sheet, as its details tell it, with a picture.
const xc40 = vehicles.items[0] as Vehicle
const volvo = xc40.sheet as Sheet
const none: Sheet = {
  ...volvo,
  colour: null,
  gearbox: null,
  drive: null,
  steering: null,
  power_kw: null,
  gross_weight_kg: null,
  seats: null,
  first_registration: null,
  picture: false,
}

function sheet(s: Sheet | null) {
  return mountWith(VehicleSheet, { props: { vehicle: { id: xc40.id, sheet: s } } }).wrapper
}

const facts = (w: ReturnType<typeof sheet>) =>
  w
    .findAll('.fact')
    .map((f) => `${f.find('dt').text()}: ${f.find('dd').text().replace(/\s/g, ' ')}`)

describe('VehicleSheet', () => {
  it('shows what the source tells, the main values first, the rest folded', async () => {
    const w = sheet(volvo)
    expect(w.find('h3').text()).toBe('Sheet')
    expect(w.find('img').attributes('src')).toBe(`api/v1/vehicles/${xc40.id}/picture`)
    expect(w.find('img').attributes('alt')).toBe('')
    expect(facts(w)).toEqual([
      'Colour: Jupiter',
      'Drive: Rear-wheel drive',
      'Power: 185 kW',
      'First registration: 15 Mar 2021',
      'Seats: 5',
    ])
    const toggle = w.find('.more-toggle')
    expect(toggle.text()).toBe('Show 3 more')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(toggle.text()).toBe('Show fewer')
    expect(facts(w).slice(5)).toEqual([
      'Gearbox: Automatic',
      'Gross weight: 2,520 kg',
      'Steering wheel: On the left',
    ])
  })

  it("shows a Tesla's own words, its name first", async () => {
    const w = sheet({
      ...none,
      name: 'Blixt',
      colour: 'Deep Blue',
      roof_colour: 'Glass',
      wheels: 'HelixV220Dark',
      steering: 'right',
      third_row: true,
      performance: true,
    })
    expect(w.find('img').exists()).toBe(false)
    expect(facts(w)).toEqual(['Name in the app: Blixt', 'Colour: Deep Blue'])
    await w.find('.more-toggle').trigger('click')
    expect(facts(w).slice(2)).toEqual([
      'Steering wheel: On the right',
      'Wheels: HelixV220Dark',
      'Roof: Glass',
      'Third row of seats: Yes',
      'Performance package: Yes',
    ])
  })

  it('shows the others without a toggle when none of the main values is told', () => {
    const w = sheet({ ...none, gearbox: 'manual', engine_cc: 1969 })
    expect(w.find('.more-toggle').exists()).toBe(false)
    expect(facts(w)).toEqual(['Gearbox: Manual', 'Engine displacement: 1,969 cm³'])
  })

  it('shows nothing of a source that tells nothing', () => {
    expect(sheet(null).find('.vehicle-sheet').exists()).toBe(false)
    expect(sheet(none).find('.vehicle-sheet').exists()).toBe(false)
    // A picture alone is worth the tile.
    expect(
      sheet({ ...none, picture: true })
        .find('.vehicle-sheet img')
        .exists(),
    ).toBe(true)
  })
})

describe('VehiclePicture', () => {
  it('goes when it fails to load, and comes back for another vehicle', async () => {
    const props = reactive({ vehicle: { id: xc40.id, sheet: volvo } })
    const { wrapper } = mountWith(VehiclePicture, { props })
    expect(wrapper.find('img.small').exists()).toBe(true)
    await wrapper.find('img').trigger('error')
    expect(wrapper.find('img').exists()).toBe(false)
    props.vehicle = { id: 'other', sheet: volvo }
    await nextTick()
    expect(wrapper.find('img').attributes('src')).toBe('api/v1/vehicles/other/picture')
  })
})
