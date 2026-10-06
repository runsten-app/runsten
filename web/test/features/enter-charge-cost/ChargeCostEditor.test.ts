import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Charge } from '@/entities/charge'
import { ChargeCostEditor } from '@/features/enter-charge-cost'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import entered from '@fixtures/charge-cost.json'
import charge from '@fixtures/charge.json'
import settings from '@fixtures/settings.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({ setChargeCost: vi.fn(), deleteChargeCost: vi.fn() }))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  ...api,
}))

const eur = { code: 'EUR', minor_digits: 2 }
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  i18n.global.locale.value = 'en'
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

function editor(c: unknown = charge, currency = eur) {
  wrapper = mountWith(ChargeCostEditor, {
    props: { vehicle: 'v1', charge: c as Charge, currency, limits: settings.limits.entered_cost },
    attachTo: document.body,
  }).wrapper
  return wrapper
}

const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const input = (id: string) => w().find<HTMLInputElement | HTMLTextAreaElement>(`#${id}`)
const toggle = () => w().find<HTMLButtonElement>('.toggle')
async function open() {
  await toggle().trigger('click')
  await flushPromises()
}
async function submit() {
  await w().find('form').trigger('submit')
  await flushPromises()
}
const summary = () =>
  w()
    .findAll('.error-summary li')
    .map((li) => li.text())
const status = () => w().find('[role="status"]').text()

describe('ChargeCostEditor', () => {
  it('opens an empty form under its button, its first field focused', async () => {
    editor()
    expect(toggle().text()).toBe('Enter the cost')
    expect(toggle().attributes('aria-expanded')).toBe('false')
    expect(toggle().attributes('aria-controls')).toBeUndefined()
    expect(w().find('form').exists()).toBe(false)
    await open()
    expect(toggle().attributes('aria-expanded')).toBe('true')
    expect(toggle().attributes('aria-controls')).toBe('charge-cost-form')
    expect(w().find('form').attributes('id')).toBe('charge-cost-form')
    expect(document.activeElement).toBe(input('charge-cost-amount').element)
    expect(input('charge-cost-amount').element.value).toBe('')
    expect(w().find('.v-text-field .v-text-field__suffix').text()).toBe('€')
  })

  it('tells what is missing in a summary that takes the focus and leads to the field', async () => {
    editor()
    await open()
    await submit()
    expect(api.setChargeCost).not.toHaveBeenCalled()
    expect(summary()).toEqual(['Amount paid: Required.'])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(input('charge-cost-amount').attributes('aria-invalid')).toBe('true')
    await w().find('.error-summary a').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(input('charge-cost-amount').element)
    // The errors follow the changes, once sent.
    await input('charge-cost-amount').setValue('1.234')
    await input('charge-cost-energy').setValue('0')
    await input('charge-cost-note').setValue('x'.repeat(501))
    expect(summary()).toEqual([
      'Amount paid: At most 2 decimals.',
      'Billed energy (optional): More than 0, at most 500 kWh.',
      'Note (optional): At most 500 characters.',
    ])
    await input('charge-cost-amount').setValue('-1')
    expect(summary()[0]).toBe('Amount paid: Between €0.00 and €10,000,000.00.')
    await input('charge-cost-amount').setValue('douze')
    expect(summary()[0]).toBe('Amount paid: Enter an amount, with a point or a comma.')
  })

  it('saves what was paid, closes, and says so on its button', async () => {
    api.setChargeCost.mockResolvedValue(entered)
    editor()
    await open()
    await input('charge-cost-amount').setValue('12,34')
    await input('charge-cost-energy').setValue('31.5')
    await input('charge-cost-note').setValue('Ionity, carte Chargemap')
    await submit()
    expect(api.setChargeCost).toHaveBeenCalledWith('v1', charge.id, {
      amount_minor: 1234,
      energy_kwh: 31.5,
      note: 'Ionity, carte Chargemap',
    })
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(toggle().element)
    expect(status()).toBe('Cost saved.')
  })

  it.each([
    [
      new ApiError(400, 'invalid_body', 'body.note: too long'),
      'Runsten refused the cost: body.note: too long',
    ],
    [
      new ApiError(404, 'not_found', ''),
      'This charge no longer exists: the charges were detected again. Go back to the list.',
    ],
    [new ApiError(0, 'network', ''), 'Runsten cannot be reached. Try again in a moment.'],
    [new ApiError(500, 'internal', ''), 'Something went wrong. Try again later.'],
  ])('tells a refusal %o in the summary, the form open', async (error, text) => {
    api.setChargeCost.mockRejectedValue(error)
    editor()
    await open()
    await input('charge-cost-amount').setValue('8')
    await submit()
    expect(summary()).toEqual([text])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(w().find('form').exists()).toBe(true)
    // A change clears it.
    await input('charge-cost-amount').setValue('9')
    expect(w().find('.error-summary').exists()).toBe(false)
  })

  it('starts from the entered cost, and a cancel sends nothing', async () => {
    editor(entered)
    expect(toggle().text()).toBe('Change the entered cost')
    expect(w().find('.delete-cost').exists()).toBe(true)
    await open()
    expect(input('charge-cost-amount').element.value).toBe('8.50')
    expect(input('charge-cost-energy').element.value).toBe('20.1')
    expect(input('charge-cost-note').element.value).toBe('Borne du parking, carte Chargemap')
    // The deletion waits for the form to close.
    expect(w().find('.delete-cost').exists()).toBe(false)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(toggle().element)
    expect(api.setChargeCost).not.toHaveBeenCalled()
    // Its button closes it too.
    await open()
    await open()
    expect(w().find('form').exists()).toBe(false)
  })

  it('refuses decimals in a currency without them', async () => {
    editor(charge, { code: 'ISK', minor_digits: 0 })
    await open()
    expect(w().find('.v-text-field__suffix').text()).toBe('ISK')
    await input('charge-cost-amount').setValue('12.5')
    await submit()
    expect(summary()).toEqual(['Amount paid: No decimals.'])
  })

  it('speaks French, and agrees', async () => {
    i18n.global.locale.value = 'fr'
    editor()
    expect(toggle().text()).toBe('Saisir le coût')
    await open()
    await input('charge-cost-amount').setValue('1,234')
    await submit()
    expect(summary()).toEqual(['Montant payé : Au plus 2 décimales.'])
  })
})

describe('ChargeCostEditor, deleting', () => {
  it('asks in a dialog, then deletes the entered cost and says so', async () => {
    api.deleteChargeCost.mockResolvedValue(undefined)
    editor(entered)
    await w().find('.delete-cost').trigger('click')
    await flushPromises()
    const dialog = document.querySelector('[role="dialog"]')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(
      document.getElementById(dialog?.getAttribute('aria-labelledby') ?? '')?.textContent?.trim(),
    ).toBe('Delete the entered cost?')
    ;(document.querySelector('.confirm-delete') as HTMLElement | null)?.click()
    await flushPromises()
    expect(api.deleteChargeCost).toHaveBeenCalledWith('v1', entered.id)
    expect(status()).toBe('Entered cost deleted.')
    expect(document.activeElement).toBe(toggle().element)
  })

  it('tells a failure in the dialog', async () => {
    api.deleteChargeCost.mockRejectedValue(new ApiError(500, 'internal', ''))
    editor(entered)
    await w().find('.delete-cost').trigger('click')
    await flushPromises()
    ;(document.querySelector('.confirm-delete') as HTMLElement | null)?.click()
    await flushPromises()
    expect(document.querySelector('[role="dialog"] .v-alert')?.textContent).toContain(
      'The cost could not be deleted.',
    )
    expect(status()).toBe('')
    const cancel = [...document.querySelectorAll('[role="dialog"] button')].find(
      (b) => b.textContent?.trim() === 'Cancel',
    ) as HTMLElement | undefined
    cancel?.click()
    await flushPromises()
    expect(api.deleteChargeCost).toHaveBeenCalledOnce()
    // Back to the button that opened it; Escape closes it too.
    expect(document.activeElement).toBe(w().find('.delete-cost').element)
    await w().find('.delete-cost').trigger('click')
    await flushPromises()
    document
      .querySelector('[role="dialog"]')
      ?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.activeElement).toBe(w().find('.delete-cost').element)
  })
})
