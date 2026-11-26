import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings } from '@/entities/connection'
import { FordForm } from '@/features/manage-ford'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import connection from '@fixtures/connection.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  startFordSignIn: vi.fn(),
  setFordCode: vi.fn(),
  deleteFord: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))
const nav = vi.hoisted(() => ({ leave: vi.fn() }))
vi.mock('@/features/manage-ford/model/leave', () => nav)

const pasteRedirect = 'http://localhost/auth/ford/callback'
// The golden connection is an instance Ford cannot return to, without a Ford connection:
// both are stated here, the tests below depending on them.
const none: ConnectionSettings = {
  ...(connection as ConnectionSettings),
  ford: null,
  ford_application: { redirect_uri: pasteRedirect, callback: false },
}
const ford = {
  last4: 'abcd',
  set_at: '2026-09-28T05:00:00Z',
  expires_at: '2027-09-28T05:00:00Z',
  refused_at: null,
}
const given: ConnectionSettings = { ...none, ford }
const clientId = '0a1b2c3d-4e5f-client-abcd'
const secret = 'S3cr3t~value-of-the-app'
const signInPage = 'https://ford.example/auth/init?client_id=x&state=STATE'
const pasted = `${pasteRedirect}?code=the-code&state=STATE`
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  nav.leave.mockReset()
  i18n.global.locale.value = 'en'
  vi.useFakeTimers({ now: Date.parse('2026-09-28T07:05:00Z'), toFake: ['Date'] })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
})

// props is reactive: the connection given back by the API replaces it, as the query does.
let props = reactive<{ connection: ConnectionSettings; off?: boolean }>({ connection: none })
function form(c: ConnectionSettings, extra: { off?: boolean } = {}) {
  props = reactive({ connection: c, ...extra })
  wrapper = mountWith(FordForm, { props, attachTo: document.body }).wrapper
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const idField = () => w().find<HTMLInputElement>('#ford-client-id')
const secretField = () => w().find<HTMLInputElement>('#ford-client-secret')
const callbackField = () => w().find<HTMLInputElement>('#ford-callback')
async function fill(id = clientId, s = secret) {
  await idField().setValue(id)
  await secretField().setValue(s)
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
const paste = { url: signInPage, state: 'STATE', redirect_uri: pasteRedirect, callback: false }

describe('FordForm', () => {
  it('without a connection, is open and tells what to create at Ford, with the redirect address', () => {
    form(none)
    const text = w().text()
    expect(text).toContain('FordConnect Query')
    expect(text).toContain('not checked on a real car yet')
    expect(text).toContain('same email address as your Ford account')
    expect(text).toContain('up to two hours')
    expect(w().find('a.guide').attributes('href')).toBe('https://runsten.app/docs/guide/ford')
    expect(w().find('a.portal').attributes('href')).toBe('https://developer.ford.com/developer-eu')
    expect(w().find('a.portal').attributes('rel')).toBe('noreferrer')
    const redirect = w().find<HTMLInputElement>('#ford-redirect')
    expect(redirect.element.value).toBe(pasteRedirect)
    expect(redirect.attributes('readonly')).toBeDefined()
    // The secret is typed as a password.
    expect(secretField().attributes('type')).toBe('password')
    expect(w().find('.cancel').exists()).toBe(false)
    expect(callbackField().exists()).toBe(false)
  })

  it('where Ford comes back by itself, sends the browser to its sign-in page', async () => {
    const own = 'https://runsten.example/auth/ford/callback'
    form({ ...none, ford_application: { redirect_uri: own, callback: true } })
    expect(w().find<HTMLInputElement>('#ford-redirect').element.value).toBe(own)
    api.startFordSignIn.mockResolvedValue({ ...paste, redirect_uri: own, callback: true })
    await fill(` ${clientId} `, ` ${secret}\n`)
    await submit()
    expect(api.startFordSignIn).toHaveBeenCalledWith(clientId, secret)
    expect(nav.leave).toHaveBeenCalledWith(signInPage)
    expect(callbackField().exists()).toBe(false)
    expect(api.setFordCode).not.toHaveBeenCalled()
  })

  it('stays here when the form was closed before the sign-in page was told', async () => {
    const own = 'https://runsten.example/auth/ford/callback'
    form({ ...given, ford_application: { redirect_uri: own, callback: true } })
    await w().find('.toggle').trigger('click')
    await flushPromises()
    let tell: (a: unknown) => void = () => undefined
    api.startFordSignIn.mockReturnValue(new Promise((resolve) => (tell = resolve)))
    await fill()
    await submit()
    await w().find('.cancel').trigger('click')
    tell({ ...paste, redirect_uri: own, callback: true })
    await flushPromises()
    expect(nav.leave).not.toHaveBeenCalled()
    expect(w().find('form').exists()).toBe(false)
  })

  it('elsewhere, has the sign-in opened in another tab and its address pasted back', async () => {
    form(none)
    api.startFordSignIn.mockResolvedValue(paste)
    await fill()
    await submit()
    expect(nav.leave).not.toHaveBeenCalled()
    const page = w().find('a.sign-in-page')
    expect(page.attributes('href')).toBe(signInPage)
    expect(page.attributes('target')).toBe('_blank')
    expect(w().text()).toContain(`an address starting with ${pasteRedirect}`)
    expect(idField().exists()).toBe(false)
    expect(document.activeElement).toBe(callbackField().element)

    // The connection the API gives back replaces the page's, as the query does.
    api.setFordCode.mockImplementation(() => {
      props.connection = given
      return Promise.resolve(given)
    })
    await callbackField().setValue(`  ${pasted} `)
    await submit()
    expect(api.setFordCode).toHaveBeenCalledWith('STATE', pasted)
    expect(status()).toBe('Your Ford is connected: the collector reads it from its next pass.')
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement?.id).toBe('ford-toggle')
  })

  it('checks the application before asking Ford, and the address before sending it', async () => {
    form(none)
    await submit()
    expect(summary()).toEqual([
      'Enter the application’s client ID.',
      'Enter the application’s secret.',
    ])
    expect(api.startFordSignIn).not.toHaveBeenCalled()
    await fill('short', 'with a space')
    expect(summary()).toEqual([
      'Too short: at least 8 characters.',
      'It holds a space or a character no client ID or secret has: copy it again from Ford’s portal.',
    ])
    expect(idField().attributes('aria-invalid')).toBe('true')

    api.startFordSignIn.mockResolvedValue(paste)
    await fill()
    await submit()
    await submit()
    expect(summary()).toEqual(['Paste the address Ford sent you to.'])
    await callbackField().setValue('http://localhost/auth/ford/callback?state=STATE')
    expect(summary()).toEqual([
      'This address holds no code: copy the whole address of the page Ford sent you to.',
    ])
    expect(api.setFordCode).not.toHaveBeenCalled()
  })

  it('tells what Ford, or Runsten, refused', async () => {
    form(none)
    for (const [code, text] of [
      [
        'source_unavailable',
        'Too many sign-ins are waiting on this instance: try again in a few minutes.',
      ],
      ['feature_unavailable', 'Ford is not offered to your account.'],
      [
        'not_admitted',
        'Something is left to do on your account before connecting a car: reload the page.',
      ],
      ['network', 'Runsten could not be reached. Try again in a moment.'],
      ['internal', 'Something went wrong on Runsten’s side. Try again in a moment.'],
    ] as const) {
      api.startFordSignIn.mockRejectedValueOnce(new ApiError(500, code, code))
      await fill()
      await submit()
      expect(summary()).toEqual([text])
    }
    for (const [code, text] of [
      [
        'authorization_expired',
        'This sign-in expired, was already used, or Ford refused its code: start again.',
      ],
      [
        'credential_refused',
        'Ford refused the client ID or the secret: check them on Ford’s developer portal, then start again.',
      ],
      [
        'no_vehicle',
        'This sign-in reads no car: add your car to your Ford account, then start again.',
      ],
      [
        'too_many_vehicles',
        'With this car, your account would have more vehicles than it may: it was not connected.',
      ],
      ['source_unavailable', 'Ford did not answer: nothing was stored. Start again in a moment.'],
    ] as const) {
      api.startFordSignIn.mockResolvedValueOnce(paste)
      await fill()
      await submit()
      api.setFordCode.mockRejectedValueOnce(new ApiError(400, code, code))
      await callbackField().setValue(pasted)
      await submit()
      expect(summary()).toEqual([text])
      // The sign-in is spent: back to the application, its fields kept.
      expect(callbackField().exists()).toBe(false)
      expect(idField().element.value).toBe(clientId)
    }
  })

  it('keeps the sign-in when the address pasted is not one, and lets the owner start again', async () => {
    form(none)
    api.startFordSignIn.mockResolvedValue(paste)
    await fill()
    await submit()
    api.setFordCode.mockRejectedValueOnce(new ApiError(400, 'invalid_body', 'invalid body'))
    await callbackField().setValue(pasted)
    await submit()
    expect(summary()).toEqual([
      'This address holds no code: copy the whole address of the page Ford sent you to.',
    ])
    expect(callbackField().exists()).toBe(true)
    await w().find('.start-again').trigger('click')
    await flushPromises()
    expect(callbackField().exists()).toBe(false)
    expect(idField().element.value).toBe(clientId)
    expect(document.activeElement).toBe(idField().element)
  })

  it('with a connection, tells it by its last four characters, when, and its end, behind a button', async () => {
    form(given)
    expect(facts()).toEqual([
      ['Your application', 'Client ID ending in abcd'],
      ['Connected', '05:00'],
      ['Sign in again before', expect.stringContaining('2027')],
    ])
    expect(w().find('form').exists()).toBe(false)
    expect(w().find('.v-alert').exists()).toBe(false)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(idField().element)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement?.id).toBe('ford-toggle')
  })

  it('warns two weeks before the year ends, once it is over, and once Ford refused', () => {
    form({ ...none, ford: { ...ford, expires_at: '2026-10-05T00:00:00Z' } })
    expect(w().find('.renew').text()).toContain('Sign in to Ford again')
    wrapper?.unmount()
    form({ ...none, ford: { ...ford, expires_at: '2026-09-27T00:00:00Z' } })
    expect(w().find('.expired').text()).toContain('Your sign-in to Ford is over')
    wrapper?.unmount()
    form({ ...none, ford: { ...ford, refused_at: '2026-09-28T06:00:00Z' } })
    expect(w().find('.refused').text()).toContain('Ford refused the connection')
  })

  it('removes the connection after a confirmation', async () => {
    form(given)
    await w().find('.delete-connection').trigger('click')
    await flushPromises()
    const dialog = document.querySelector('.v-dialog')
    expect(dialog?.textContent).toContain('Remove your Ford connection?')
    api.deleteFord.mockResolvedValue(undefined)
    dialog?.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(api.deleteFord).toHaveBeenCalled()
    props.connection = none
    await flushPromises()
    expect(status()).toBe('The Ford connection is removed.')
    expect(w().find('form').exists()).toBe(true)
  })

  it('turned off, tells a connection made before and lets it be removed, without a form', async () => {
    form(given, { off: true })
    expect(facts()).toHaveLength(3)
    expect(w().find('.toggle').exists()).toBe(false)
    expect(w().find('form').exists()).toBe(false)
    await w().find('.delete-connection').trigger('click')
    await flushPromises()
    api.deleteFord.mockResolvedValue(undefined)
    document.querySelector<HTMLButtonElement>('.v-dialog .confirm-delete')?.click()
    await flushPromises()
    props.connection = none
    await flushPromises()
    // No form to go to: the focus is given to the status, which tells the removal.
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(w().find('[role="status"]').element)
    expect(status()).toBe('The Ford connection is removed.')
  })
})
