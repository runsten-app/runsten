import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { VariantOption, Vehicle } from '@/entities/vehicle'
import { VehicleModelForm } from '@/features/edit-vehicle-model'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import variants from '@fixtures/variants.json'
import chosen from '@fixtures/vehicle-model.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({ fetchVariants: vi.fn(), setVehicleModel: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  ...api,
}))

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  i18n.global.locale.value = 'en'
})
// Mounted in the document, for the focus: unmounted even when a test fails, or its IDs
// would stay for the next one.
const mounted: VueWrapper[] = []
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount())
})

// The EX30 of the fixtures, whose driver chose the Single Motor Extended Range and its
// standard charger; its details fit it and the Twin Motor.
const ex30 = chosen as Vehicle
const options = variants.items as VariantOption[]
// The same EX30, left to the recognition, which found the Single Motor Extended Range.
const detected: Vehicle = {
  ...ex30,
  model: { ...ex30.model, variant_source: 'detected', ac_max_kw: null },
}
const recognizing = options.map((o) => ({ ...o, candidate: o.id === 'ex30-er-2024' }))

async function form(vehicle: Vehicle, list: VariantOption[] = options) {
  api.fetchVariants.mockResolvedValue(list)
  const m = mountWith(VehicleModelForm, {
    props: { vehicle, label: 'My EX30' },
    attachTo: document.body,
  })
  await flushPromises()
  mounted.push(m.wrapper)
  return m.wrapper
}

const radios = (w: VueWrapper, group: string) =>
  w.findAll<HTMLInputElement>(`.${group} input[type="radio"]`)
const labels = (w: VueWrapper, group: string) =>
  w.findAll(`.${group} .v-radio label`).map((l) => l.text().replace(/\s+/g, ' '))
const checked = (w: VueWrapper, group: string) =>
  radios(w, group).findIndex((r) => r.element.checked)
async function pick(w: VueWrapper, group: string, i: number) {
  radios(w, group)[i]?.element.click()
  await flushPromises()
}
async function save(w: VueWrapper) {
  const button = w.find<HTMLButtonElement>('button.save')
  button.element.focus()
  await w.find('form').trigger('submit')
  await flushPromises()
  return button.element
}

describe('VehicleModelForm', () => {
  it('shows what the vehicle reports, and the variants in a fieldset', async () => {
    const w = await form(ex30)
    expect(w.find('h2').text()).toBe('My EX30')
    expect(w.findAll('dt').map((d) => d.text())).toEqual([
      'VIN',
      'Model reported',
      'Model year reported',
    ])
    expect(w.findAll('dd').map((d) => d.text())).toEqual(['YV1SMLT0000DT0004', 'EX30', '2024'])
    const legends = w.findAll('legend').map((l) => l.text())
    expect(legends).toEqual(['Variant', 'Onboard charger'])
    const group = w.find('.variants [role="radiogroup"]')
    expect(group.attributes('aria-labelledby')).toBe(w.findAll('legend')[0]?.attributes('id'))
    expect(api.fetchVariants).toHaveBeenCalledWith(ex30.id)

    const shown = labels(w, 'variants')
    expect(shown).toHaveLength(options.length + 1)
    expect(shown[0]).toBe(
      'Automatic recognition No variant recognized: several fit, or none. Choose yours below.',
    )
    expect(shown[1]).toBe(
      'EX30 Single Motor Extended Range, 2024 to 2026 ' +
        '69 kWh battery, 64 kWh usable · AC 11 kW, 22 kW as an option · DC 153 kW ' +
        'According to public sources',
    )
    // A variant still on sale, whose battery has no known usable capacity.
    expect(shown.at(-1)).toContain(', since 2027')
    // The driver's choice.
    expect(checked(w, 'variants')).toBe(1)
    expect(labels(w, 'chargers')).toEqual([
      'Not stated: 22 kW assumed, the more powerful',
      '11 kW, standard',
      '22 kW, optional',
    ])
    expect(checked(w, 'chargers')).toBe(1)
  })

  it('marks the recognized variant, and assumes the more powerful charger', async () => {
    const w = await form(detected, recognizing)
    expect(checked(w, 'variants')).toBe(0)
    const shown = labels(w, 'variants')
    expect(shown[0]).toBe('Automatic recognition Recognized: EX30 Single Motor Extended Range')
    expect(shown[1]).toContain(
      'EX30 Single Motor Extended Range, 2024 to 2026 · recognized automatically',
    )
    expect(shown.filter((l) => l.includes('recognized automatically'))).toHaveLength(1)
    // The recognized variant has the option: the charger is asked, not stated yet.
    expect(checked(w, 'chargers')).toBe(0)
  })

  it('asks the charger only when the variant in effect has an option', async () => {
    const w = await form(ex30)
    expect(w.find('.chargers').exists()).toBe(true)
    // The Single Motor: 11 kW only.
    const lfp = options.findIndex((o) => o.id === 'ex30-lfp-2024') + 1
    await pick(w, 'variants', lfp)
    expect(w.find('.chargers').exists()).toBe(false)
    expect(w.findAll('legend').map((l) => l.text())).toEqual(['Variant'])
    // Automatic, nothing recognized: no variant, no charger either.
    await pick(w, 'variants', 0)
    expect(w.find('.chargers').exists()).toBe(false)
  })

  it('sends the choice, and says so where the focus stays', async () => {
    api.setVehicleModel.mockResolvedValue(chosen)
    const w = await form(ex30)
    await pick(w, 'chargers', 2) // 22 kW
    const button = await save(w)
    expect(api.setVehicleModel).toHaveBeenCalledWith(ex30.id, {
      variant_id: 'ex30-er-2024',
      ac_max_kw: 22,
    })
    expect(w.find('[role="status"]').text()).toBe('Variant saved.')
    expect(document.activeElement).toBe(button)
    expect(button.getAttribute('aria-label')).toBe('Save the variant of My EX30')
    expect(w.find('.error-summary').exists()).toBe(false)
    // A change clears the message.
    await pick(w, 'chargers', 0)
    expect(w.find('[role="status"]').text()).toBe('')
    await save(w)
    expect(api.setVehicleModel).toHaveBeenLastCalledWith(ex30.id, {
      variant_id: 'ex30-er-2024',
      ac_max_kw: null,
    })
  })

  it('goes back to the recognition, forgetting a charger the variant lacks', async () => {
    api.setVehicleModel.mockResolvedValue(chosen)
    const w = await form(ex30)
    await pick(w, 'variants', 0)
    await save(w)
    expect(api.setVehicleModel).toHaveBeenCalledWith(ex30.id, { variant_id: null, ac_max_kw: null })
  })

  it('refuses a variant no longer offered, in a summary that leads to it', async () => {
    const gone: Vehicle = {
      ...ex30,
      model: {
        ...ex30.model,
        variant: { ...chosen.model.variant, id: 'ex30-gone' },
        ac_max_kw: null,
      },
    }
    const w = await form(gone)
    await save(w)
    expect(api.setVehicleModel).not.toHaveBeenCalled()
    const summary = w.find('.error-summary')
    expect(document.activeElement).toBe(summary.element)
    expect(summary.text()).toContain('The variant could not be saved')
    expect(summary.find('li').text()).toBe(
      'Variant: This variant is no longer offered: choose another one.',
    )
    expect(w.find('.variants .v-messages').text()).toBe(
      'This variant is no longer offered: choose another one.',
    )
    await summary.find('a').trigger('click')
    expect(document.activeElement).toBe(radios(w, 'variants')[0]?.element)
    // Choosing one clears the error.
    await pick(w, 'variants', 1)
    expect(w.find('.error-summary').exists()).toBe(false)
  })

  it.each([
    [
      new ApiError(400, 'invalid_body', 'body.variant_id: unknown variant'),
      'Runsten refused the choice: body.variant_id: unknown variant',
    ],
    [new ApiError(404, 'not_found', ''), 'This vehicle no longer exists.'],
    [new ApiError(0, 'network', ''), 'Runsten cannot be reached. Try again in a moment.'],
    [new ApiError(500, 'internal', ''), 'Something went wrong. Try again later.'],
  ])('tells a refusal, %o, in the summary', async (error, text) => {
    api.setVehicleModel.mockRejectedValue(error)
    const w = await form(ex30)
    await save(w)
    const summary = w.find('.error-summary')
    expect(summary.find('li').text()).toBe(text)
    expect(document.activeElement).toBe(summary.element)
    expect(w.find('[role="status"]').text()).toBe('')
  })

  it.each<[string, Vehicle, string]>([
    [
      'a family the catalog lacks',
      { ...ex30, model: { ...ex30.model, family: 'EX-SIM', variant: null, variant_source: null } },
      "Runsten's catalog has no variant of the EX-SIM: it lists the battery-electric Volvo and Polestar models only.",
    ],
    [
      'details never read',
      vehicles.items[1] as Vehicle,
      'Runsten has not read the details of this vehicle yet: its variant can be chosen once they are.',
    ],
  ])('offers no variant for %s', async (_, vehicle, text) => {
    const w = await form(vehicle, [])
    expect(w.find('.none').text()).toBe(text)
    expect(w.find('form').exists()).toBe(false)
    expect(w.find('[role="status"]').exists()).toBe(true)
  })

  it('tells when the variants cannot be read', async () => {
    api.fetchVariants.mockRejectedValue(new ApiError(500, 'internal', ''))
    const { wrapper } = mountWith(VehicleModelForm, { props: { vehicle: ex30, label: 'My EX30' } })
    await flushPromises()
    expect(wrapper.find('.v-alert').text()).toBe(
      'The variants could not be loaded. Try again in a moment.',
    )
  })

  it('names where the figures come from, figure by figure', async () => {
    const mixed = options.map((o, i) =>
      i === 0
        ? {
            ...o,
            levels: {
              ...o.levels,
              gross_kwh: 'manufacturer' as const,
              ac_max_kw: 'manufacturer' as const,
            },
          }
        : {
            ...o,
            levels: {
              gross_kwh: 'manufacturer' as const,
              net_kwh: null,
              ac_max_kw: 'manufacturer' as const,
              ac_option_kw: null,
              dc_max_kw: 'manufacturer' as const,
            },
          },
    )
    const w = await form(ex30, mixed)
    const shown = labels(w, 'variants')
    expect(shown[1]).toContain(
      'According to the manufacturer; usable capacity, optional charger, DC power: according to public sources',
    )
    expect(shown[2]).toContain('According to the manufacturer')
    i18n.global.locale.value = 'fr'
    await flushPromises()
    expect(labels(w, 'variants')[2]).toContain("D'après le constructeur")
  })
})
