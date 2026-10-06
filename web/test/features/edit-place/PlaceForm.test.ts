import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { h } from 'vue'
import { RouterView } from 'vue-router'
import type { Place } from '@/entities/place'
import { PlaceForm } from '@/features/edit-place'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import place from '@fixtures/place.json'
import settings from '@fixtures/settings.json'
import { mountWith, testRouter } from '@test/utils'

const api = vi.hoisted(() => ({
  createPlace: vi.fn(),
  replacePlace: vi.fn(),
  deletePlace: vi.fn(),
  fetchUnpricedCharges: vi.fn(),
}))
vi.mock('@/entities/place', async (original) => ({
  ...(await original<typeof import('@/entities/place')>()),
  ...api,
}))

const eur = { code: 'EUR', minor_digits: 2 }
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  api.fetchUnpricedCharges.mockResolvedValue({ charges: 0, first_day: null })
  i18n.global.locale.value = 'en'
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-27T10:00:00Z'))
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
})

// form mounts the form in a route, as the app does: it guards leaving it.
async function form(
  props: {
    place?: Place | null
    lat?: number
    lon?: number
    limits?: typeof settings.limits
    defaultEfficiency?: typeof settings.default_efficiency
  } = {},
) {
  const saved = vi.fn()
  const deleted = vi.fn()
  const Host = {
    render: () =>
      h(
        PlaceForm,
        {
          place: null,
          currency: eur,
          limits: settings.limits,
          defaultEfficiency: settings.default_efficiency,
          ...props,
          onSaved: saved,
          onDeleted: deleted,
        },
        {
          position: ({ setPosition }: { setPosition: (p: object, from: string) => void }) =>
            h('button', {
              type: 'button',
              class: 'fill',
              onClick: () => setPosition({ lat: 59.3293, lon: 18.0686 }, 'XC40'),
            }),
        },
      ),
  }
  const router = testRouter([{ path: '/form', name: 'form', component: Host }])
  await router.push('/form')
  const m = mountWith(RouterView, { router, attachTo: document.body })
  wrapper = m.wrapper
  await flushPromises()
  return { ...m, saved, deleted }
}

const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const input = (id: string) => w().find<HTMLInputElement>(`#${id}`)
async function fill(values: Record<string, string>) {
  for (const [id, v] of Object.entries(values)) await input(id).setValue(v)
}
async function submit() {
  await w().find('form').trigger('submit')
  await flushPromises()
}
const summary = () =>
  w()
    .findAll('.error-summary li')
    .map((li) => li.text())
const described = (id: string) => {
  const ids = input(id).attributes('aria-describedby')?.split(' ') ?? []
  return ids.map((d) => document.getElementById(d)?.textContent?.trim()).join(' ')
}

const valid = {
  'place-name': 'Home',
  'place-lat': '45,764',
  'place-lon': '4.8357',
  'place-v0-price': '0.2142',
}

describe('PlaceForm, a new place', () => {
  it('starts at the position given, 100 m, in the browser zone, priced from today', async () => {
    await form({ lat: 45.764, lon: 4.8357 })
    expect(input('place-lat').element.value).toBe('45.76400')
    expect(input('place-lon').element.value).toBe('4.83570')
    expect(input('place-radius').element.value).toBe('100')
    expect(w().findComponent({ name: 'VAutocomplete' }).props('modelValue')).toBe('UTC')
    expect(input('place-v0-from').element.value).toBe('2026-09-27')
    expect(w().find('legend').exists()).toBe(true)
    expect(w().text()).toContain('Price from 27 Sept 2026')
    expect(w().text()).toContain('No time window: the base price applies at all times.')
    // The price in the account's currency.
    expect(w().find('.v-text-field__suffix').text()).toBe('m')
    expect(w().text()).toContain('€/kWh')
    // Nothing to delete yet; the efficiency's default is told.
    expect(w().find('.delete-place').exists()).toBe(false)
    expect(w().text()).toContain('Empty for the default: 0.88 in AC, 0.95 in DC.')
  })

  it('fills the position from a vehicle, and says so', async () => {
    await form()
    await w().find('button.fill').trigger('click')
    await flushPromises()
    expect(input('place-lat').element.value).toBe('59.32930')
    expect(input('place-lon').element.value).toBe('18.06860')
    expect(w().find('[role="status"]').text()).toBe('Position of XC40 filled in.')
  })

  it('tells nothing wrong before it is sent, then everything, focused, each line to its field', async () => {
    await form()
    await fill({ 'place-lat': '91' })
    expect(w().find('.error-summary').exists()).toBe(false)
    expect(w().find('.v-input--error').exists()).toBe(false)
    await submit()
    expect(api.createPlace).not.toHaveBeenCalled()
    expect(document.activeElement?.classList.contains('error-summary')).toBe(true)
    expect(w().find('.error-summary h2').text()).toBe('The place could not be saved')
    expect(summary()).toEqual([
      'Name: Required.',
      'Latitude: Between -90 and 90.',
      'Longitude: Required.',
      'Price from 27 Sept 2026, Base price: Required.',
    ])
    // Each field tells its own error.
    expect(described('place-lat')).toContain('Between -90 and 90.')
    expect(input('place-lat').attributes('aria-invalid')).toBe('true')
    await w().find('.error-summary a[href="#place-lon"]').trigger('click')
    expect(document.activeElement).toBe(input('place-lon').element)
    // Errors follow the changes.
    await fill({ 'place-lat': '45.7' })
    expect(summary()).not.toContain('Latitude: Between -90 and 90.')
  })

  it('names the window of an error, and opens Advanced for one there', async () => {
    await form()
    await fill(valid)
    await w().find('.add-window').trigger('click')
    await flushPromises()
    // The new window takes the focus, on its first day.
    expect(document.activeElement).toBe(input('place-v0-w0-days').element)
    await w().find('.v-btn[aria-pressed="false"]').trigger('click') // Weekdays
    await w().find('.v-btn[aria-pressed="false"]').trigger('click') // Every day → Weekend
    await fill({
      'place-v0-w0-from': '22:00',
      'place-v0-w0-to': '06:00',
      'place-v0-w0-price': '0,1234567',
    })
    expect(w().text()).toContain('Crosses midnight: ends the next day.')
    const details = w().find('details').element
    await w().find('details summary').trigger('click')
    details.open = false
    await fill({ 'place-efficiency': '1.5' })
    await submit()
    expect(summary()).toEqual([
      'Charging efficiency: More than 0, at most 1.',
      'Price from 27 Sept 2026, Time window 1, Price: At most 5 decimals.',
    ])
    await w().find('.error-summary a[href="#place-efficiency"]').trigger('click')
    await flushPromises()
    expect(details.open).toBe(true)
    expect(document.activeElement).toBe(input('place-efficiency').element)
  })

  it('tells a window without a day or a month in its fieldset', async () => {
    await form()
    await fill(valid)
    await w().find('.add-window').trigger('click')
    await flushPromises()
    await fill({
      'place-v0-w0-from': '00:00',
      'place-v0-w0-to': '00:00',
      'place-v0-w0-price': '0.19',
    })
    expect(w().text()).toContain('Lasts 24 hours.')
    for (const box of w().findAll<HTMLInputElement>('.price-window input[type="checkbox"]'))
      if (box.element.checked) await box.setValue(false)
    await submit()
    expect(summary()).toEqual([
      'Price from 27 Sept 2026, Time window 1, Days: Choose at least one day.',
      'Price from 27 Sept 2026, Time window 1, Months: Choose at least one month, or all year.',
    ])
    const groups = w().findAll('.price-window fieldset.group')
    expect(groups.map((g) => g.attributes('aria-invalid'))).toEqual(['true', 'true'])
    expect(
      document.getElementById(groups[0]?.attributes('aria-describedby') ?? '')?.textContent,
    ).toContain('Choose at least one day.')
    // A month chosen, the error goes.
    await w()
      .findAll<HTMLInputElement>('.price-window input[type="checkbox"]')
      .at(-1)
      ?.setValue(true)
    expect(summary()).toHaveLength(1)
  })

  it('creates the place with the body of the contract, and stays on the form', async () => {
    api.createPlace.mockResolvedValue(place)
    const { saved } = await form()
    await fill({ ...valid, 'place-radius': '150', 'place-max-power': '7,4' })
    await input('place-without-position').setValue(true)
    await w().find('.add-window').trigger('click')
    await flushPromises()
    await fill({
      'place-v0-w0-from': '22:00',
      'place-v0-w0-to': '06:00',
      'place-v0-w0-price': '0.1589',
    })
    const save = w().find<HTMLButtonElement>('button.save')
    save.element.focus()
    await submit()
    expect(api.createPlace).toHaveBeenCalledWith({
      name: 'Home',
      position: { lat: 45.764, lon: 4.8357 },
      radius_m: 150,
      time_zone: 'UTC',
      without_position: true,
      max_power_kw: 7.4,
      efficiency: null,
      tariff: [
        {
          valid_from: '2026-09-27',
          price_per_kwh: 0.2142,
          windows: [
            {
              days: ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'],
              from: '22:00',
              to: '06:00',
              months: [],
              price_per_kwh: 0.1589,
            },
          ],
        },
      ],
    })
    expect(saved).toHaveBeenCalledWith(place)
    expect(w().find('[role="status"]').text()).toBe('Place saved.')
    expect(document.activeElement).toBe(save.element)
    expect(w().find('.error-summary').exists()).toBe(false)
    // It exists now: it can be deleted, and saved again in place.
    expect(w().find('.delete-place').exists()).toBe(true)
    api.replacePlace.mockResolvedValue(place)
    await submit()
    expect(api.replacePlace).toHaveBeenCalledWith(place.id, expect.anything())
  })

  it.each([
    [
      new ApiError(409, 'without_position_taken', ''),
      'Another place already takes the charges without a position.',
      'place-without-position',
    ],
    [
      new ApiError(409, 'too_many_places', ''),
      `An account has at most ${settings.limits.places} places.`,
      undefined,
    ],
    [
      new ApiError(400, 'invalid_body', 'validation failed: body.name: too long'),
      'Runsten refused the place: validation failed: body.name: too long',
      undefined,
    ],
    [new ApiError(404, 'not_found', ''), 'This place no longer exists.', undefined],
    [
      new ApiError(502, 'unavailable', ''),
      'Runsten cannot be reached. Try again in a moment.',
      undefined,
    ],
    [
      new ApiError(0, 'network', ''),
      'Runsten cannot be reached. Try again in a moment.',
      undefined,
    ],
    [new ApiError(500, 'internal', ''), 'Something went wrong. Try again later.', undefined],
  ])('explains %o in the summary', async (error, text, field) => {
    api.createPlace.mockRejectedValue(error)
    await form()
    await fill(valid)
    await submit()
    expect(summary()).toEqual([text])
    expect(document.activeElement?.classList.contains('error-summary')).toBe(true)
    const link = w().find('.error-summary a')
    expect(link.exists() ? link.attributes('href') : undefined).toBe(
      field ? `#${field}` : undefined,
    )
    // A change clears it.
    await fill({ 'place-name': 'Home 2' })
    expect(w().find('.error-summary').exists()).toBe(false)
  })

  it('tells a name too long, a word for a number and a time zone cleared', async () => {
    await form()
    await fill({ ...valid, 'place-name': 'x'.repeat(61), 'place-lon': 'east' })
    w().findComponent({ name: 'VAutocomplete' }).vm.$emit('update:modelValue', null)
    await submit()
    expect(summary()).toEqual([
      'Name: At most 60 characters.',
      'Longitude: Enter a number, with a point or a comma.',
      'Time zone: Required.',
    ])
  })

  // The browser's date and time fields give nothing else, but a place read from the API
  // could: the rules are told all the same.
  it.each<[string, Partial<Place>, string[]]>([
    ['no price', { tariff: [] }, ['Tariff: At least one price.']],
    [
      'too many prices',
      {
        tariff: Array.from({ length: 51 }, (_, i) => ({
          valid_from: new Date(Date.UTC(2020, 0, 1 + i)).toISOString().slice(0, 10),
          price_per_kwh: 0.2,
          windows: [],
        })),
      },
      ['Tariff: At most 50.'],
    ],
    [
      'a wrong day and time',
      {
        tariff: [
          {
            valid_from: '2026-13-01',
            price_per_kwh: 0.2,
            windows: [
              { days: ['mon'], from: '24:00', to: '06:00', months: [], price_per_kwh: 0.1 },
            ],
          },
        ],
      },
      ['New price, From: Not a valid date.', 'New price, Time window 1, From: Not a valid time.'],
    ],
  ])('tells %s in a place read', async (_, edit, want) => {
    await form({ place: { ...(place as Place), ...edit } })
    await submit()
    expect(summary()).toEqual(want)
  })
})

describe('PlaceForm, the tariff', () => {
  it('holds a place as it is', async () => {
    await form({ place: place as Place })
    expect(input('place-name').element.value).toBe('Maison de Tante Agathe')
    expect(input('place-max-power').element.value).toBe('11')
    expect(w().findAll('.tariff-version')).toHaveLength(2)
    expect(w().findAll('.price-window')).toHaveLength(4)
    const legends = w()
      .findAll('.tariff-version > .v-card-text > fieldset > legend')
      .map((l) => l.text())
    expect(legends).toEqual(['Price from 1 Feb 2026', 'Price from 1 Aug 2026'])
    expect(input('place-v1-w0-months').element.checked).toBe(false)
    expect(input('place-v1-w2-months').element.checked).toBe(true)
  })

  it('adds a price from the latest, focused on its day, and removes one', async () => {
    await form({ place: place as Place })
    await w().find('.add-version').trigger('click')
    await flushPromises()
    expect(w().findAll('.tariff-version')).toHaveLength(3)
    expect(document.activeElement).toBe(input('place-v2-from').element)
    expect(input('place-v2-from').element.value).toBe('2026-09-27')
    expect(input('place-v2-price').element.value).toBe('0.2142')
    expect(w().findAll('.tariff-version').at(2)?.findAll('.price-window')).toHaveLength(3)
    await w().findAll('.remove-version').at(0)?.trigger('click')
    await flushPromises()
    expect(w().findAll('.tariff-version')).toHaveLength(2)
    expect(document.activeElement).toBe(w().find('.add-version').element)
    expect(input('place-v0-from').element.value).toBe('2026-08-01')
  })

  it('never removes the last price', async () => {
    await form()
    expect(w().find('.remove-version').exists()).toBe(false)
  })

  it('moves and removes windows, the focus following', async () => {
    await form({ place: place as Place })
    const froms = () => [0, 1, 2].map((j) => input(`place-v1-w${j}-from`).element.value)
    expect(froms()).toEqual(['23:00', '02:00', '00:00'])
    expect(w().find('#place-v1-w0-up').attributes('disabled')).toBeDefined()
    expect(w().find('#place-v1-w2-down').attributes('disabled')).toBeDefined()
    expect(w().find('#place-v1-w1-up').attributes('aria-label')).toBe('Move up: time window 2')

    await w().find('#place-v1-w1-up').trigger('click')
    await flushPromises()
    expect(froms()).toEqual(['02:00', '23:00', '00:00'])
    // At the top, Up is disabled: the focus goes to Down.
    expect(document.activeElement?.id).toBe('place-v1-w0-down')

    await w().find('#place-v1-w0-down').trigger('click')
    await flushPromises()
    expect(froms()).toEqual(['23:00', '02:00', '00:00'])
    expect(document.activeElement?.id).toBe('place-v1-w1-down')

    await w().find('#place-v1-w1-down').trigger('click')
    await flushPromises()
    expect(froms()).toEqual(['23:00', '00:00', '02:00'])
    expect(document.activeElement?.id).toBe('place-v1-w2-up')

    await w().find('#place-v1-w0-up').trigger('click')
    await w().find('#place-v1-w2-up').trigger('click')
    await flushPromises()
    expect(froms()).toEqual(['23:00', '02:00', '00:00'])
    expect(document.activeElement?.id).toBe('place-v1-w1-up')

    await w().findAll('.tariff-version').at(1)?.find('.remove-window').trigger('click')
    await flushPromises()
    expect(w().findAll('.tariff-version').at(1)?.findAll('.price-window')).toHaveLength(2)
    expect(document.activeElement?.id).toBe('place-v1-add-window')
  })

  it('sets the days by shortcut, each saying whether it is the one', async () => {
    await form({ place: place as Place })
    const presets = () =>
      w()
        .findAll('#place-v1-w2 [role="group"] .v-btn')
        .map((b) => b.attributes('aria-pressed'))
    expect(presets()).toEqual(['false', 'false', 'true'])
    await w().findAll('#place-v1-w2 [role="group"] .v-btn').at(1)?.trigger('click')
    expect(presets()).toEqual(['false', 'true', 'false'])
    expect(
      w()
        .findAll<HTMLInputElement>('#place-v1-w2 input[type="checkbox"]')
        .slice(0, 7)
        .map((c) => c.element.checked),
    ).toEqual([true, true, true, true, true, false, false])
  })

  it('refuses two prices on the same day', async () => {
    await form({ place: place as Place })
    await fill({ 'place-v1-from': '2026-02-01' })
    await submit()
    expect(summary()).toEqual([
      'Price from 1 Feb 2026, From: Another price starts on the same day.',
    ])
  })

  // 50 prices, one of 24 windows: over 5 s on the CI's two cores.
  it('stops adding at the limits', { timeout: 20_000 }, async () => {
    const many = {
      ...place,
      tariff: Array.from({ length: 50 }, (_, i) => ({
        valid_from: new Date(Date.UTC(2020, 0, 1 + i)).toISOString().slice(0, 10),
        price_per_kwh: 0.2,
        windows: i
          ? []
          : Array.from({ length: 24 }, () => ({ ...place.tariff[0]?.windows[0], days: ['mon'] })),
      })),
    } as Place
    await form({ place: many })
    expect(w().find('.add-version').attributes('disabled')).toBeDefined()
    expect(w().find('#place-v0-add-window').attributes('disabled')).toBeDefined()
    expect(w().text()).toContain('At most 24 time windows per price.')
  })

  it('follows the limits and efficiencies of the settings, not a copy of them', async () => {
    await form({
      limits: {
        ...settings.limits,
        radius_m: { min: 50, max: 500, default: 250 },
        versions_per_place: 1,
        windows_per_version: 0,
      },
      defaultEfficiency: { ac: 0.8, dc: 0.9 },
    })
    expect(input('place-radius').element.value).toBe('250')
    expect(w().find('.add-version').attributes('disabled')).toBeDefined()
    expect(w().find('#place-v0-add-window').attributes('disabled')).toBeDefined()
    expect(w().text()).toContain('Empty for the default: 0.8 in AC, 0.9 in DC.')
  })
})

describe('PlaceForm, charges without a price', () => {
  const unpriced = () => w().find('.unpriced')

  it('offers to start the tariff on the day of the charges before it', async () => {
    api.fetchUnpricedCharges.mockResolvedValue({ charges: 2, first_day: '2026-01-20' })
    await form({ place: place as Place })
    expect(api.fetchUnpricedCharges).toHaveBeenCalledWith(place.id)
    expect(unpriced().text()).toContain(
      'No price of this tariff applies to 2 charges at this place: they started before 1 Feb 2026, the day of the first price.',
    )
    const button = unpriced().find('button.start-on')
    expect(button.text()).toBe('Start the tariff on 20 Jan 2026')
    await button.trigger('click')
    await flushPromises()
    // The first price moved, not the latest; the focus is on its day, the alert gone.
    expect(input('place-v0-from').element.value).toBe('2026-01-20')
    expect(input('place-v1-from').element.value).toBe('2026-08-01')
    expect(document.activeElement).toBe(input('place-v0-from').element)
    expect(unpriced().exists()).toBe(false)
    expect(w().find('.status').text()).toBe(
      'The first price now starts on 20 Jan 2026: save to give the charges their cost.',
    )
  })

  it('tells one charge in the singular, and nothing once the draft covers it', async () => {
    api.fetchUnpricedCharges.mockResolvedValue({ charges: 1, first_day: '2026-01-31' })
    await form({ place: place as Place })
    expect(unpriced().text()).toContain(
      'No price of this tariff applies to one charge at this place',
    )
    await input('place-v0-from').setValue('2026-01-31')
    expect(unpriced().exists()).toBe(false)
    await input('place-v0-from').setValue('2026-02-0')
    // A day being typed moves nothing: the other version is the first one written.
    expect(unpriced().text()).toContain('before 1 Aug 2026')
  })

  it('says nothing when every charge has a price', async () => {
    await form({ place: place as Place })
    expect(api.fetchUnpricedCharges).toHaveBeenCalled()
    expect(unpriced().exists()).toBe(false)
  })

  it('asks once a new place is saved, and tells it on the same form', async () => {
    api.createPlace.mockResolvedValue({ ...place, tariff: [place.tariff[0]] })
    api.fetchUnpricedCharges.mockResolvedValue({ charges: 3, first_day: '2026-09-26' })
    await form()
    expect(api.fetchUnpricedCharges).not.toHaveBeenCalled()
    await fill(valid)
    await submit()
    expect(api.fetchUnpricedCharges).toHaveBeenCalledWith(place.id)
    expect(unpriced().text()).toContain('3 charges at this place: they started before 27 Sept 2026')
    await unpriced().find('button.start-on').trigger('click')
    await flushPromises()
    expect(input('place-v0-from').element.value).toBe('2026-09-26')
  })
})

describe('PlaceForm, deleting', () => {
  it('asks in a dialog, then deletes', async () => {
    api.deletePlace.mockResolvedValue(undefined)
    const { deleted } = await form({ place: place as Place })
    await w().find('.delete-place').trigger('click')
    await flushPromises()
    const dialog = document.querySelector('[role="dialog"]')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(
      document.getElementById(dialog?.getAttribute('aria-labelledby') ?? '')?.textContent?.trim(),
    ).toBe('Delete Maison de Tante Agathe?')
    ;(document.querySelector('.confirm-delete') as HTMLElement | null)?.click()
    await flushPromises()
    expect(api.deletePlace).toHaveBeenCalledWith(place.id)
    expect(deleted).toHaveBeenCalledOnce()
  })

  it('tells a failure in the dialog, and a cancel deletes nothing', async () => {
    api.deletePlace.mockRejectedValue(new ApiError(500, 'internal', ''))
    const { deleted } = await form({ place: place as Place })
    await w().find('.delete-place').trigger('click')
    await flushPromises()
    ;(document.querySelector('.confirm-delete') as HTMLElement | null)?.click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"] .v-alert')?.textContent).toContain(
      'The place could not be deleted.',
    )
    expect(deleted).not.toHaveBeenCalled()
    const cancel = [...document.querySelectorAll('[role="dialog"] button')].find(
      (b) => b.textContent?.trim() === 'Cancel',
    ) as HTMLElement | undefined
    cancel?.click()
    await flushPromises()
    expect(deleted).not.toHaveBeenCalled()
  })
})

describe('PlaceForm, leaving', () => {
  it('asks before leaving unsaved changes, not otherwise', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { router } = await form({ place: place as Place })
    await router.push('/')
    expect(confirm).not.toHaveBeenCalled()
    await router.push('/form')
    await flushPromises()
    await fill({ 'place-name': 'Changed' })
    await router.push('/')
    expect(confirm).toHaveBeenCalledWith('Leave without saving the changes?')
    expect(router.currentRoute.value.path).toBe('/form')
    confirm.mockReturnValue(true)
    await router.push('/')
    expect(router.currentRoute.value.path).toBe('/')
    confirm.mockRestore()
  })

  it('asks the browser before closing the tab with unsaved changes', async () => {
    await form()
    const clean = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)
    await fill({ 'place-name': 'Home' })
    const dirty = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirty)
    expect(dirty.defaultPrevented).toBe(true)
  })
})
