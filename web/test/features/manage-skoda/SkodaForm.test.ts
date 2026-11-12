import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings } from '@/entities/connection'
import { SkodaForm } from '@/features/manage-skoda'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import connection from '@fixtures/connection-skoda.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  setSkodaCredential: vi.fn(),
  deleteSkodaCredential: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

// The golden key: given at 05:00 on 28 September, ending in "-key", expiring a year later.
const given = connection as ConnectionSettings
const none: ConnectionSettings = { ...given, skoda: null }
const withKey = (k: Partial<NonNullable<ConnectionSettings['skoda']>>): ConnectionSettings => {
  const skoda = given.skoda
  if (!skoda) throw new Error('fixture')
  return { ...given, skoda: { ...skoda, ...k } }
}
const key = 'runsten-dev-skoda-key'
const vin = 'TMBJC9NY0RF000001'
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  i18n.global.locale.value = 'en'
  vi.useFakeTimers({ now: Date.parse('2026-09-28T07:05:00Z'), toFake: ['Date'] })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
})

// props is reactive: the connection given back by the API replaces it, as the query does.
let props = reactive<{ connection: ConnectionSettings; vins?: string[]; off?: boolean }>({
  connection: none,
})
function form(c: ConnectionSettings, extra: { vins?: string[]; off?: boolean } = {}) {
  props = reactive({ connection: c, ...extra })
  wrapper = mountWith(SkodaForm, { props, attachTo: document.body }).wrapper
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const apiKey = () => w().find<HTMLInputElement>('#skoda-api-key')
const vins = () => w().find<HTMLTextAreaElement>('#skoda-vins')
async function fill(k: string, v: string) {
  await apiKey().setValue(k)
  await vins().setValue(v)
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
const facts = () =>
  w()
    .findAll('.fact')
    .map((f) => [f.find('dt').text(), f.find('dd').text()])

describe('SkodaForm', () => {
  it('without a key, is open with the steps to the app’s keys and the guide', () => {
    form(none)
    expect(w().text()).toContain(
      'Runsten reads your Škoda with a key you create in the MyŠkoda app',
    )
    const keys = w().find('a.keys')
    expect(keys.attributes('href')).toBe('https://go.skoda.eu/api-keys')
    expect(keys.attributes('rel')).toBe('noreferrer')
    expect(w().find('a.guide').attributes('href')).toBe('https://runsten.app/docs/guide/skoda')
    expect(w().findAll('.steps li')).toHaveLength(3)
    expect(w().text()).toContain('personal use only')
    // The key is typed as a password: neither shown nor kept by the browser's autofill.
    expect(apiKey().attributes('type')).toBe('password')
    expect(apiKey().attributes('autocomplete')).toBe('new-password')
    expect(w().find('.cancel').exists()).toBe(false)
  })

  it('with a key, tells it by its last four characters, when it was given and when it expires', async () => {
    form(given, { vins: [vin] })
    expect(facts()).toEqual([
      ['Your key', 'Key ending in -key'],
      ['Given', '05:00'],
      ['Expires', 'Tuesday, 28 September 2027'],
    ])
    expect(w().text()).not.toContain(key)
    expect(w().find('.renew').exists()).toBe(false)
    expect(w().find('form').exists()).toBe(false)
    // Replaced behind a button, the account's VINs filled in.
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(apiKey().element)
    expect(vins().element.value).toBe(vin)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(w().find('.toggle').element)
  })

  it('fills the VINs in once read, but keeps a list the user emptied', async () => {
    form(none, { vins: [] })
    props.vins = [vin]
    await flushPromises()
    expect(vins().element.value).toBe(vin)
    await vins().setValue('')
    props.vins = [vin, 'TMBNC7NY5SF083963']
    await flushPromises()
    expect(vins().element.value).toBe('')
  })

  it('keeps Cancel away while a replacement is being saved', async () => {
    let answer: (c: ConnectionSettings) => void = () => {}
    api.setSkodaCredential.mockReturnValue(
      new Promise<ConnectionSettings>((resolve) => {
        answer = resolve
      }),
    )
    form(given, { vins: [vin] })
    await w().find('.toggle').trigger('click')
    await fill(key, vin)
    await w().find('form').trigger('submit')
    await flushPromises()
    expect(w().find('.cancel').attributes('disabled')).toBeDefined()
    answer(given)
    await flushPromises()
  })

  it('tells an expiry not told yet', () => {
    form(withKey({ expires_at: null }))
    expect(facts()[2]).toEqual(['Expires', 'Told at the next read'])
  })

  it.each([
    [
      { expires_at: '2026-10-05T07:00:00Z' },
      '.renew',
      'Renew your key',
      'It expires on Monday, 5 October 2026.',
    ],
    [
      { expires_at: '2026-09-27T07:00:00Z' },
      '.expired',
      'Your key has expired',
      'It expired on Sunday, 27 September 2026',
    ],
    [{ refused_at: '2026-09-28T05:30:00Z' }, '.refused', 'Your key was refused', '05:30'],
  ])('warns of a key %j', (k, selector, title, text) => {
    form(withKey(k))
    expect(w().find(`${selector} .v-alert-title`).text()).toBe(title)
    expect(w().find(selector).text()).toContain(text)
  })

  it('tells what is wrong before sending, each value under its field', async () => {
    form(none)
    await submit()
    expect(api.setSkodaCredential).not.toHaveBeenCalled()
    expect(summary()).toEqual(['Paste the API key.', 'Give the VIN of each car the key reads.'])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(apiKey().attributes('aria-invalid')).toBe('true')
    await fill(key, 'TMBJC9NY0RF00000O')
    expect(summary()).toEqual([
      'TMBJC9NY0RF00000O is not a VIN: 17 letters and digits, without I, O nor Q.',
    ])
  })

  it('saves the key with its VINs, and says so', async () => {
    api.setSkodaCredential.mockResolvedValue(given)
    form(none)
    await fill(` ${key} `, 'tmbjc9ny0rf000001')
    await submit()
    expect(api.setSkodaCredential).toHaveBeenCalledWith(
      { api_key: key, vins: [vin] },
      expect.anything(),
    )
    props.connection = given
    await flushPromises()
    expect(status()).toBe('Key saved: Runsten reads its cars from its next pass.')
    expect(w().find('form').exists()).toBe(false)
  })

  it.each([
    [
      new ApiError(400, 'credential_refused', ''),
      'Škoda refused this key: copy it again, and check that it still exists in the MyŠkoda app.',
      '.api_key',
    ],
    [
      new ApiError(400, 'key_expired', ''),
      'This key has expired: create a new one in the MyŠkoda app.',
      '.api_key',
    ],
    [
      new ApiError(409, 'vin_not_covered', `the key does not read the vehicle ${vin}: tick it`),
      `The key does not read ${vin}: tick this car when you create the key in the MyŠkoda app, or check its VIN.`,
      '.vins',
    ],
    [
      new ApiError(400, 'invalid_body', ''),
      'The key or a VIN was refused: copy them again.',
      '.vins',
    ],
  ])('tells %s under its field', async (e, text, field) => {
    api.setSkodaCredential.mockRejectedValue(e)
    form(none)
    await fill(key, vin)
    await submit()
    expect(summary()).toEqual([text])
    expect(w().find(`${field} .v-messages`).text()).toBe(text)
  })

  it.each([
    [
      new ApiError(409, 'too_many_vehicles', ''),
      'With these cars, your account would have more vehicles than it may: the key was not saved.',
    ],
    [
      new ApiError(403, 'not_admitted', ''),
      'Your account must take a step first, such as accepting terms: reload the page. The key was not saved.',
    ],
    [
      new ApiError(502, 'source_unavailable', ''),
      'The MyŠkoda Public API did not answer: the key was not saved. Try again in a moment.',
    ],
    [
      new ApiError(403, 'feature_unavailable', ''),
      'Škoda is not offered on this instance: the key was not saved.',
    ],
    [new ApiError(0, 'network', ''), 'Runsten could not be reached. Try again in a moment.'],
    [new ApiError(500, 'internal', ''), 'The key could not be saved. Try again in a moment.'],
  ])('tells %s', async (e, text) => {
    api.setSkodaCredential.mockRejectedValue(e)
    form(none)
    await fill(key, vin)
    await submit()
    expect(summary()).toEqual([text])
  })

  it('removes the key once confirmed, the vehicles kept, the focus on the next one', async () => {
    api.deleteSkodaCredential.mockResolvedValue(undefined)
    form(given)
    await w().find('.delete-credential').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Remove your MyŠkoda key?')
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(api.deleteSkodaCredential).toHaveBeenCalled()
    props.connection = none
    await flushPromises()
    expect(status()).toBe('Key removed.')
    expect(document.activeElement).toBe(apiKey().element)
  })

  it('takes no key while the source is off, a key given before still removable', () => {
    form(given, { off: true })
    expect(w().find('.toggle').exists()).toBe(false)
    expect(w().find('form').exists()).toBe(false)
    expect(w().find('.delete-credential').exists()).toBe(true)
  })
})
