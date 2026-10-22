import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings } from '@/entities/connection'
import { PolestarForm } from '@/features/manage-polestar'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import connection from '@fixtures/connection-polestar.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  setPolestarCredential: vi.fn(),
  deletePolestarCredential: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

const none: ConnectionSettings = { ...(connection as ConnectionSettings), polestar: null }
// Given at 05:00 on 28 September: it expires on 27 December.
const given = connection as ConnectionSettings
const secret = 'runsten-dev-secret'
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
let props = reactive({ connection: none })
function form(c: ConnectionSettings) {
  props = reactive({ connection: c })
  wrapper = mountWith(PolestarForm, { props, attachTo: document.body }).wrapper
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const input = (id: string) => w().find<HTMLInputElement>(`#polestar-${id}`)
async function fill(clientId: string, clientSecret: string, accountId: string) {
  await input('client-id').setValue(clientId)
  await input('client-secret').setValue(clientSecret)
  await input('account-id').setValue(accountId)
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

describe('PolestarForm', () => {
  it('without a credential, is open with the steps to the Data Portal and the guide', () => {
    form(none)
    expect(w().text()).toContain(
      "Runsten reads your Polestar with a credential you create on Polestar's Data Portal",
    )
    expect(w().text()).toContain('Polestar support is in beta')
    const portal = w().find('a.portal')
    expect(portal.attributes('href')).toBe('https://data-portal.polestar.com/')
    expect(portal.attributes('rel')).toBe('noreferrer')
    expect(w().find('a.guide').attributes('href')).toBe('https://runsten.app/docs/guide/polestar')
    expect(w().findAll('.steps li')).toHaveLength(5)
    expect(w().text()).toContain(
      'battery, odometer, location, availability and target state of charge',
    )
    // The secret is typed as a password: neither shown nor kept by the browser's autofill.
    expect(input('client-secret').attributes('type')).toBe('password')
    expect(input('client-secret').attributes('autocomplete')).toBe('new-password')
    expect(w().find('.cancel').exists()).toBe(false)
  })

  it('with a credential, tells it by its last four characters and its dates, replaced behind a button', async () => {
    form(given)
    const facts = w()
      .findAll('.fact')
      .map((f) => [f.find('dt').text(), f.find('dd').text()])
    expect(facts).toEqual([
      ['Your credential', 'Client ID ending in -dev'],
      ['Given', '05:00'],
      ['Expires', 'Sunday, 27 December 2026'],
    ])
    expect(w().text()).not.toContain(secret)
    expect(w().find('.renew').exists()).toBe(false)
    expect(w().find('form').exists()).toBe(false)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(input('client-id').element)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(w().find('.toggle').element)
  })

  it('reminds to renew it two weeks before it expires, and once past', async () => {
    vi.setSystemTime(Date.parse('2026-12-14T06:00:00Z'))
    form(given)
    expect(w().find('.renew .v-alert-title').text()).toBe('Renew your Polestar credential')
    expect(w().find('.renew').text()).toContain('this one expires on Sunday, 27 December 2026')
    expect(w().find('.toggle').classes()).toContain('text-primary')
    wrapper?.unmount()
    vi.setSystemTime(Date.parse('2026-12-28T06:00:00Z'))
    form(given)
    expect(w().find('.renew').text()).toContain(
      'this one should have expired on Sunday, 27 December 2026',
    )
  })

  it('tells a credential refused, rather than its expiry', () => {
    vi.setSystemTime(Date.parse('2026-12-20T06:00:00Z'))
    const polestar = given.polestar && { ...given.polestar, refused_at: '2026-12-20T05:00:00Z' }
    form({ ...given, polestar })
    expect(w().find('.refused .v-alert-title').text()).toBe('Your Polestar credential was refused')
    expect(w().find('.refused').text()).toContain('05:00')
    expect(w().find('.renew').exists()).toBe(false)
  })

  it('tells what is wrong before sending, each value under its field', async () => {
    form(none)
    await submit()
    expect(api.setPolestarCredential).not.toHaveBeenCalled()
    expect(summary()).toEqual([
      'Paste the Client ID.',
      'Paste the Client secret.',
      'Paste the Account ID.',
    ])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(input('account-id').attributes('aria-invalid')).toBe('true')
    await fill('runsten dev', secret, 'abc')
    expect(summary()).toEqual([
      'The Client ID has no space: copy it again.',
      'The Account ID has at least 4 characters: copy it whole.',
    ])
  })

  it('saves the credential, trimmed, in the body, and says so', async () => {
    api.setPolestarCredential.mockResolvedValue(given)
    form(none)
    await fill(' runsten-dev ', secret, 'acct-1234 ')
    await submit()
    expect(api.setPolestarCredential).toHaveBeenCalledWith({
      client_id: 'runsten-dev',
      client_secret: secret,
      account_id: 'acct-1234',
    })
    props.connection = given
    await flushPromises()
    expect(status()).toBe("Credential saved: its vehicles are read from the collector's next pass.")
    expect(w().find('form').exists()).toBe(false)
  })

  it('tells a credential Polestar refused, under its first field', async () => {
    api.setPolestarCredential.mockRejectedValue(new ApiError(400, 'credential_refused', 'refused'))
    form(none)
    await fill('runsten-dev', 'wrong-secret', 'acct-1234')
    await submit()
    const refused =
      'Polestar refused this credential: check its three values, and that it has not expired.'
    expect(summary()).toEqual([refused])
    expect(w().find('.client_id .v-messages').text()).toBe(refused)
    await input('client-secret').setValue(secret)
    expect(summary()).toEqual([])
  })

  it.each([
    [
      new ApiError(409, 'no_vehicle', ''),
      'This credential reads no vehicle: check on the Data Portal that it reads your car, then save it again.',
    ],
    [
      new ApiError(409, 'too_many_vehicles', ''),
      "With this credential's vehicles, your account would have more vehicles than it may: the credential was not saved.",
    ],
    [
      new ApiError(403, 'not_admitted', ''),
      'Your account must take a step first, such as accepting terms: reload the page. The credential was not saved.',
    ],
    [
      new ApiError(502, 'source_unavailable', ''),
      "Polestar's Data Portal did not answer: the credential was not saved. Try again in a moment.",
    ],
    [new ApiError(400, 'invalid_body', ''), 'This credential was refused: copy its values again.'],
    [new ApiError(0, 'network', ''), 'Runsten could not be reached. Try again in a moment.'],
    [
      new ApiError(500, 'internal', ''),
      'The credential could not be saved. Try again in a moment.',
    ],
  ])('tells %s', async (e, text) => {
    api.setPolestarCredential.mockRejectedValue(e)
    form(none)
    await fill('runsten-dev', secret, 'acct-1234')
    await submit()
    expect(summary()).toEqual([text])
  })

  it('removes the credential once confirmed, the vehicles kept, the focus on the next one', async () => {
    api.deletePolestarCredential.mockResolvedValue(undefined)
    form(given)
    await w().find('.delete-credential').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Remove your Polestar credential?')
    expect(document.body.textContent).toContain('Your Polestar vehicles stay, with their history')
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(api.deletePolestarCredential).toHaveBeenCalled()
    props.connection = none
    await flushPromises()
    expect(status()).toBe('Credential removed.')
    expect(document.activeElement).toBe(input('client-id').element)
  })

  it('keeps the dialog open on a failed removal', async () => {
    api.deletePolestarCredential.mockRejectedValue(new ApiError(500, 'internal', ''))
    form(given)
    await w().find('.delete-credential').trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(document.body.textContent).toContain('The credential could not be removed.')
  })

  it('is written in French and Swedish too', () => {
    i18n.global.locale.value = 'fr'
    form(none)
    expect(w().find('h3').text()).toBe('Créer votre identifiant')
    wrapper?.unmount()
    i18n.global.locale.value = 'sv'
    form(none)
    expect(w().find('h3').text()).toBe('Skapa dina uppgifter')
  })
})
