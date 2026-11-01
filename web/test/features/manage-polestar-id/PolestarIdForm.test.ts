import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings } from '@/entities/connection'
import { PolestarIdForm } from '@/features/manage-polestar-id'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import connection from '@fixtures/connection.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  startPolestarIdSignIn: vi.fn(),
  setPolestarIdCode: vi.fn(),
  setPolestarIdPassword: vi.fn(),
  deletePolestarId: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

const none: ConnectionSettings = { ...(connection as ConnectionSettings), polestar_id: null }
const given: ConnectionSettings = {
  ...none,
  polestar_id: { mode: 'token', last4: 'a1b2', set_at: '2026-09-28T05:00:00Z', refused_at: null },
}
const authorization = {
  url: 'https://polestarid.eu.polestar.com/as/authorization.oauth2?state=ST4TE',
  state: 'ST4TE',
}
const callback = 'https://www.polestar.com/sign-in-callback?code=the-code&state=ST4TE'
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  api.startPolestarIdSignIn.mockResolvedValue(authorization)
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
  wrapper = mountWith(PolestarIdForm, { props, attachTo: document.body }).wrapper
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
async function submit() {
  await w().find('form').trigger('submit')
  await flushPromises()
}
async function choose(mode: 'token' | 'password') {
  await w().find(`#polestar-id-mode-${mode}`).setValue(true)
  await flushPromises()
}
async function start() {
  await w().find('.start').trigger('click')
  await flushPromises()
}
const summary = () =>
  w()
    .findAll('.error-summary li')
    .map((li) => li.text())
const status = () => w().find('[role="status"]').text()

describe('PolestarIdForm', () => {
  it('without a connection, is open on the sign-in at Polestar, with its steps and the guide', () => {
    form(none)
    expect(w().text()).toContain("Runsten can read the Polestar app's API with your Polestar ID")
    expect(w().text()).toContain('not the position')
    expect(w().text()).toContain('In beta')
    expect(w().find('a.guide').attributes('href')).toBe(
      'https://runsten.app/docs/guide/polestar-id',
    )
    expect(w().find<HTMLInputElement>('#polestar-id-mode-token').element.checked).toBe(true)
    expect(w().findAll('.steps li')).toHaveLength(4)
    expect(w().find('.sign-in-page').exists()).toBe(false)
    expect(w().find('#polestar-id-callback').exists()).toBe(true)
    expect(w().find('#polestar-id-password').exists()).toBe(false)
    expect(w().find('.cancel').exists()).toBe(false)
  })

  it('gives the link to Polestar, then exchanges the address pasted back', async () => {
    api.setPolestarIdCode.mockResolvedValue(given)
    form(none)
    await start()
    const link = w().find('a.sign-in-page')
    expect(link.attributes('href')).toBe(authorization.url)
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noreferrer')
    await w().find('#polestar-id-callback').setValue(` ${callback} `)
    await submit()
    expect(api.setPolestarIdCode).toHaveBeenCalledWith('ST4TE', callback)
    props.connection = given
    await flushPromises()
    expect(status()).toBe('Polestar ID connected: Runsten reads its cars from its next pass.')
    expect(w().find('form').exists()).toBe(false)
  })

  it('tells what is missing before sending', async () => {
    form(none)
    await submit()
    expect(api.setPolestarIdCode).not.toHaveBeenCalled()
    expect(summary()).toEqual([
      "Get the sign-in link first, then sign in on Polestar's page.",
      'Paste the address of the sign-in-callback.',
    ])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    await start()
    await w().find('#polestar-id-callback').setValue('https://www.polestar.com/uk/auth/error')
    expect(summary()).toEqual([
      'This address holds no code: copy the whole address of the sign-in-callback request.',
    ])
    expect(w().find('#polestar-id-callback').attributes('aria-invalid')).toBe('true')
  })

  it('asks for a new sign-in once the code is refused', async () => {
    api.setPolestarIdCode.mockRejectedValue(new ApiError(400, 'credential_refused', ''))
    form(none)
    await start()
    await w().find('#polestar-id-callback').setValue(callback)
    await submit()
    expect(summary()).toEqual([
      'The Polestar ID refused this code: it may have been used already. Get a new sign-in link and sign in again.',
    ])
    // The sign-in is spent: its link gives way to the button again.
    expect(w().find('.sign-in-page').exists()).toBe(false)
    expect(w().find('.start').exists()).toBe(true)
  })

  it('tells a sign-in too many pending', async () => {
    api.startPolestarIdSignIn.mockRejectedValue(new ApiError(503, 'source_unavailable', ''))
    form(none)
    await start()
    expect(w().find('.start-error').text()).toBe(
      'Too many sign-ins are pending on this instance. Try again in a few minutes.',
    )
  })

  it('in the password mode, says what is kept, and sends the email and password', async () => {
    api.setPolestarIdPassword.mockResolvedValue({
      ...given,
      polestar_id: given.polestar_id && { ...given.polestar_id, mode: 'password' },
    })
    form(none)
    await choose('password')
    expect(w().find('.password-warning').text()).toContain("Polestar's terms forbid")
    expect(w().find('#polestar-id-callback').exists()).toBe(false)
    const pw = w().find('#polestar-id-password')
    expect(pw.attributes('type')).toBe('password')
    expect(pw.attributes('autocomplete')).toBe('new-password')
    await submit()
    expect(summary()).toEqual([
      "Enter your Polestar ID's email.",
      "Enter your Polestar ID's password.",
    ])
    await w().find('#polestar-id-email').setValue('owner@example.com')
    await pw.setValue('secret')
    await submit()
    expect(api.setPolestarIdPassword).toHaveBeenCalledWith('owner@example.com', 'secret')
  })

  it.each([
    [
      new ApiError(400, 'credential_refused', ''),
      'The Polestar ID refused this email or password.',
    ],
    [new ApiError(409, 'no_vehicle', ''), 'This Polestar ID has no car: nothing was connected.'],
    [
      new ApiError(502, 'source_unavailable', ''),
      'The Polestar ID did not answer: nothing was connected. Try again in a moment.',
    ],
    [
      new ApiError(403, 'feature_unavailable', ''),
      'The Polestar ID is not offered on this instance.',
    ],
    [
      new ApiError(500, 'internal', ''),
      'The Polestar ID could not be connected. Try again in a moment.',
    ],
  ])('tells %s', async (e, text) => {
    api.setPolestarIdPassword.mockRejectedValue(e)
    form(none)
    await choose('password')
    await w().find('#polestar-id-email').setValue('owner@example.com')
    await w().find('#polestar-id-password').setValue('secret')
    await submit()
    expect(summary()).toEqual([text])
  })

  it('with a connection, tells its mode, connects again behind a button, and is removed', async () => {
    api.deletePolestarId.mockResolvedValue(undefined)
    form(given)
    const facts = w()
      .findAll('.fact')
      .map((f) => [f.find('dt').text(), f.find('dd').text()])
    expect(facts).toEqual([
      ['Connected with', "A sign-in on Polestar's page"],
      ['Connected', '05:00'],
    ])
    expect(w().find('form').exists()).toBe(false)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(true)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(w().find('.toggle').element)

    await w().find('.delete-connection').trigger('click')
    await flushPromises()
    await vi.waitFor(() => expect(document.querySelector('.confirm-delete')).not.toBeNull())
    ;(document.querySelector('.confirm-delete') as HTMLElement).click()
    await flushPromises()
    expect(api.deletePolestarId).toHaveBeenCalled()
  })

  it('tells a connection refused', () => {
    form({
      ...given,
      polestar_id: given.polestar_id && {
        ...given.polestar_id,
        refused_at: '2026-09-28T06:00:00Z',
      },
    })
    expect(w().find('.refused .v-alert-title').text()).toBe('Your Polestar ID was refused')
    expect(w().find('.refused').text()).toContain('06:00')
  })
})
