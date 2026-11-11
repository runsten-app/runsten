import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings, PleosBrand } from '@/entities/connection'
import { PleosForm } from '@/features/manage-pleos'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import connection from '@fixtures/connection-pleos.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  setPleosCredential: vi.fn(),
  deletePleosCredential: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

// The golden key of the Kia section: given at 05:00 on 28 September by its last four
// characters, its client ID ending in "-dev".
const given = connection as ConnectionSettings
const none: ConnectionSettings = {
  ...(connection as ConnectionSettings),
  pleos: { hyundai: null, kia: null, genesis: null },
}
const brand: PleosBrand = 'kia'
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
let props = reactive<{ connection: ConnectionSettings; brand: PleosBrand }>({
  connection: none,
  brand,
})
function form(c: ConnectionSettings, of: PleosBrand = brand) {
  props = reactive({ connection: c, brand: of })
  wrapper = mountWith(PleosForm, { props, attachTo: document.body }).wrapper
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const input = (id: string) => w().find<HTMLInputElement>(`#pleos-kia-${id}`)
async function fill(clientId: string, clientSecret: string) {
  await input('client-id').setValue(clientId)
  await input('client-secret').setValue(clientSecret)
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

describe('PleosForm', () => {
  it('without a key, is open with the steps to the Playground and the guide', () => {
    form(none)
    expect(w().text()).toContain(
      'Runsten reads your Hyundai, Kia or Genesis with a key you create in the Pleos Playground',
    )
    const portal = w().find('a.portal')
    expect(portal.attributes('href')).toBe('https://developers.pleos.ai/')
    expect(portal.attributes('rel')).toBe('noreferrer')
    expect(w().find('a.guide').attributes('href')).toBe(
      'https://runsten.app/docs/guide/hyundai-kia',
    )
    expect(w().findAll('.steps li')).toHaveLength(4)
    expect(w().text()).toContain('Bluelink, Kia Connect or Genesis Connected')
    expect(w().text()).toContain('registered when it was issued')
    // The secret is typed as a password: neither shown nor kept by the browser's autofill.
    expect(input('client-secret').attributes('type')).toBe('password')
    expect(input('client-secret').attributes('autocomplete')).toBe('new-password')
    expect(w().find('.cancel').exists()).toBe(false)
  })

  it('with a key, tells it by its last four characters and when it was given, replaced behind a button', async () => {
    form(given)
    const facts = w()
      .findAll('.fact')
      .map((f) => [f.find('dt').text(), f.find('dd').text()])
    expect(facts).toEqual([
      ['Your key', 'Client ID ending in -dev'],
      ['Given', '05:00'],
    ])
    expect(w().text()).not.toContain(secret)
    // The API tells no expiry: the Playground does, and none is shown.
    expect(w().find('.renew').exists()).toBe(false)
    expect(w().text()).not.toContain('Expires')
    expect(w().find('form').exists()).toBe(false)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(input('client-id').element)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(w().find('.toggle').element)
  })

  it('tells a key refused', () => {
    const key = given.pleos?.kia
    const kia = key ? { ...key, refused_at: '2026-09-28T05:30:00Z' } : null
    form({ ...given, pleos: { hyundai: null, kia, genesis: null } })
    expect(w().find('.refused .v-alert-title').text()).toBe('Your key was refused')
    expect(w().find('.refused').text()).toContain('05:30')
  })

  it('tells what is wrong before sending, each value under its field', async () => {
    form(none)
    await submit()
    expect(api.setPleosCredential).not.toHaveBeenCalled()
    expect(summary()).toEqual(['Paste the Client ID.', 'Paste the Client secret.'])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(input('client-id').attributes('aria-invalid')).toBe('true')
    await fill('runsten dev', 'sec')
    expect(summary()).toEqual([
      'The Client ID has no space: copy it again.',
      'The Client secret has at least 4 characters: copy it whole.',
    ])
  })

  it('saves the key, trimmed, in the body of its brand, and says so', async () => {
    api.setPleosCredential.mockResolvedValue(given)
    form(none)
    await fill(' runsten-dev ', secret)
    await submit()
    expect(api.setPleosCredential).toHaveBeenCalledWith('kia', {
      client_id: 'runsten-dev',
      client_secret: secret,
    })
    props.connection = given
    await flushPromises()
    expect(status()).toBe('Key saved: Runsten reads its cars from its next pass.')
    expect(w().find('form').exists()).toBe(false)
  })

  it('keeps the removal away while a key is being saved, whatever is typed meanwhile', async () => {
    let answer: (c: ConnectionSettings) => void = () => {}
    api.setPleosCredential.mockReturnValue(
      new Promise<ConnectionSettings>((resolve) => {
        answer = resolve
      }),
    )
    form(given)
    await w().find('.toggle').trigger('click')
    await fill('runsten-dev', secret)
    await submit()
    const remove = () => w().find('.delete-credential').attributes('disabled')
    expect(remove()).toBeDefined()
    // An edit resets the mutation's own state: the request still runs.
    await fill('runsten-dev-2', secret)
    expect(remove()).toBeDefined()
    answer(given)
    await flushPromises()
    expect(remove()).toBeUndefined()
  })

  it('tells a key Pleos refused, under its first field', async () => {
    api.setPleosCredential.mockRejectedValue(new ApiError(400, 'credential_refused', 'refused'))
    form(none)
    await fill('runsten-dev', 'wrong-secret')
    await submit()
    const refused =
      'Pleos refused this key: check its two values, and that it still exists in the Playground.'
    expect(summary()).toEqual([refused])
    expect(w().find('.client_id .v-messages').text()).toBe(refused)
    await input('client-secret').setValue(secret)
    expect(summary()).toEqual([])
  })

  it.each([
    [
      new ApiError(409, 'no_vehicle', ''),
      'This key reads no vehicle: it lists only the cars registered when it was issued, and only with the primary user’s account. Check the brand’s tab in the Playground, then save it again.',
    ],
    [
      new ApiError(409, 'too_many_vehicles', ''),
      'With this key’s cars, your account would have more vehicles than it may: the key was not saved.',
    ],
    [
      new ApiError(403, 'not_admitted', ''),
      'Your account must take a step first, such as accepting terms: reload the page. The key was not saved.',
    ],
    [
      new ApiError(502, 'source_unavailable', ''),
      'The Pleos Vehicle Data API did not answer: the key was not saved. Try again in a moment.',
    ],
    [new ApiError(400, 'invalid_body', ''), 'This key was refused: copy its values again.'],
    [new ApiError(0, 'network', ''), 'Runsten could not be reached. Try again in a moment.'],
    [new ApiError(500, 'internal', ''), 'The key could not be saved. Try again in a moment.'],
  ])('tells %s', async (e, text) => {
    api.setPleosCredential.mockRejectedValue(e)
    form(none)
    await fill('runsten-dev', secret)
    await submit()
    expect(summary()).toEqual([text])
  })

  it('removes the key once confirmed, the vehicles kept, the focus on the next one', async () => {
    api.deletePleosCredential.mockResolvedValue(undefined)
    form(given)
    await w().find('.delete-credential').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Remove your Kia key?')
    expect(document.body.textContent).toContain(
      'Your Kia stays, with its history, but is no longer read',
    )
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(api.deletePleosCredential).toHaveBeenCalled()
    props.connection = none
    await flushPromises()
    expect(status()).toBe('Key removed.')
    expect(document.activeElement).toBe(input('client-id').element)
  })
})
