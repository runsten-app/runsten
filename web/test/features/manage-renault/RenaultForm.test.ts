import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings } from '@/entities/connection'
import { RenaultForm } from '@/features/manage-renault'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  signInRenault: vi.fn(),
  setRenaultCode: vi.fn(),
  deleteRenault: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

const none: ConnectionSettings = {
  volvo: true,
  connected: false,
  client_id: 'runsten-dev',
  instance_key: null,
  api_key: null,
  polestar: null,
  tesla: null,
  polestar_id: null,
  bmw: null,
  pleos: null,
  skoda: null,
  renault: null,
}
const renault = { last4: '1a2b', set_at: '2026-09-28T05:00:00Z', refused_at: null }
const given: ConnectionSettings = { ...none, renault }
const email = 'me@example.com'
const secret = 'correct horse'
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
let props = reactive<{ connection: ConnectionSettings; off?: boolean }>({ connection: none })
function form(c: ConnectionSettings, extra: { off?: boolean } = {}) {
  props = reactive({ connection: c, ...extra })
  wrapper = mountWith(RenaultForm, { props, attachTo: document.body }).wrapper
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const emailField = () => w().find<HTMLInputElement>('#renault-email')
const passwordField = () => w().find<HTMLInputElement>('#renault-password')
const codeField = () => w().find<HTMLInputElement>('#renault-code')
async function fill(e = email, p = secret) {
  await emailField().setValue(e)
  await passwordField().setValue(p)
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
const sentTo = 'e***@example.com'
const codeSent = { code_sent_to: sentTo, connection: null, others: 0 }

describe('RenaultForm', () => {
  it('without a sign-in, is open and tells what the access is and is not', () => {
    form(none)
    const text = w().text()
    expect(text).toContain('Renault, Dacia or Alpine through the account of the My Renault app')
    expect(text).toContain('MyDacia and MyAlpine')
    expect(text).toContain('unofficial')
    expect(text).toContain('Runsten never stores it, only the session Renault returns')
    expect(text).toContain('one-time code')
    expect(text).toContain('every 10 minutes, every 5 while it charges')
    expect(text).toContain('Trips are approximate')
    expect(text).toContain('Home Assistant')
    expect(w().find('a.guide').attributes('href')).toBe('https://runsten.app/docs/guide/renault')
    expect(w().find('a.guide').attributes('rel')).toBe('noreferrer')
    // The password is typed as a password, neither shown nor kept by the browser.
    expect(passwordField().attributes('type')).toBe('password')
    expect(passwordField().attributes('autocomplete')).toBe('new-password')
    expect(w().find('.cancel').exists()).toBe(false)
  })

  it('with a sign-in, tells it by its last four characters and when, behind a button', async () => {
    form(given)
    expect(facts()).toEqual([
      ['Your account', 'Account ending in 1a2b'],
      ['Signed in', '05:00'],
    ])
    expect(w().find('form').exists()).toBe(false)
    expect(w().find('.refused').exists()).toBe(false)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(emailField().element)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(w().find('.toggle').element)
  })

  it('asks to sign in again once Renault refused the sign-in', () => {
    form({ ...none, renault: { ...renault, refused_at: '2026-09-28T05:30:00Z' } })
    expect(w().find('.refused .v-alert-title').text()).toBe('Your sign-in was refused')
    expect(w().find('.refused').text()).toContain('05:30')
  })

  it('tells what is wrong before sending, each value under its field', async () => {
    form(none)
    await submit()
    expect(api.signInRenault).not.toHaveBeenCalled()
    expect(summary()).toEqual([
      'Enter the email of your My Renault account.',
      'Enter your password.',
    ])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(emailField().attributes('aria-invalid')).toBe('true')
    await emailField().setValue('nope')
    expect(summary()[0]).toBe('This is not an email address.')
  })

  it('signs in with the interface language, and says so', async () => {
    i18n.global.locale.value = 'fr'
    api.signInRenault.mockResolvedValue({ code_sent_to: null, connection: given, others: 0 })
    form(none)
    await fill(` ${email} `)
    await submit()
    expect(api.signInRenault).toHaveBeenCalledWith(
      { email, password: secret, language: 'fr' },
      expect.anything(),
    )
    props.connection = given
    await flushPromises()
    expect(status()).toBe('Connecté : Runsten lit vos voitures dès sa prochaine passe.')
    expect(w().find('form').exists()).toBe(false)
  })

  it('tells how many vehicles of the account are not electric, so not read', async () => {
    api.signInRenault.mockResolvedValue({ code_sent_to: null, connection: given, others: 2 })
    form(none)
    await fill()
    await submit()
    props.connection = given
    await flushPromises()
    expect(w().find('.others').text()).toBe(
      '2 vehicles of the account are not electric and are not read.',
    )
  })

  it('tells one other vehicle in the singular, and none when all are read', async () => {
    api.signInRenault.mockResolvedValue({ code_sent_to: null, connection: given, others: 1 })
    form(none)
    await fill()
    await submit()
    props.connection = given
    await flushPromises()
    expect(w().find('.others').text()).toBe(
      '1 vehicle of the account is not electric and is not read.',
    )
    wrapper?.unmount()
    form(given)
    expect(w().find('.others').exists()).toBe(false)
  })

  describe('the one-time code', () => {
    async function toCodeStep() {
      api.signInRenault.mockResolvedValue(codeSent)
      form(none)
      await fill()
      await submit()
    }

    it('asks for it on a step of its own, naming the address it was sent to', async () => {
      await toCodeStep()
      expect(w().find('.code-sent').text()).toBe(`Renault sent a one-time code to ${sentTo}.`)
      expect(emailField().exists()).toBe(false)
      expect(passwordField().exists()).toBe(false)
      expect(document.activeElement).toBe(codeField().element)
      expect(codeField().attributes('autocomplete')).toBe('one-time-code')
      expect(api.setRenaultCode).not.toHaveBeenCalled()
    })

    it('checks the code before sending it', async () => {
      await toCodeStep()
      await submit()
      expect(summary()).toEqual(['Enter the code Renault sent.'])
      await codeField().setValue('12')
      expect(summary()).toEqual(['The code is 4 to 8 digits.'])
      expect(api.setRenaultCode).not.toHaveBeenCalled()
    })

    it('completes the sign-in with the code', async () => {
      api.setRenaultCode.mockResolvedValue({ code_sent_to: null, connection: given, others: 2 })
      await toCodeStep()
      await codeField().setValue('123 456')
      await submit()
      expect(api.setRenaultCode).toHaveBeenCalledWith('123456', expect.anything())
      props.connection = given
      await flushPromises()
      expect(status()).toBe('Signed in: Runsten reads your cars from its next pass.')
      expect(w().find('form').exists()).toBe(false)
    })

    it('keeps the step open on a wrong code, under its field', async () => {
      api.setRenaultCode.mockRejectedValue(new ApiError(400, 'wrong_code', ''))
      await toCodeStep()
      await codeField().setValue('123456')
      await submit()
      const text = 'Renault refused this code: check it, or go back to sign in again for a new one.'
      expect(summary()).toEqual([text])
      expect(w().find('.code .v-messages').text()).toBe(text)
      expect(codeField().exists()).toBe(true)
      expect(codeField().element.value).toBe('123456')
    })

    it('goes back to the sign-in when its ten minutes are over', async () => {
      api.setRenaultCode.mockRejectedValue(new ApiError(409, 'authorization_expired', ''))
      await toCodeStep()
      await codeField().setValue('123456')
      await submit()
      expect(summary()).toEqual(["The code's ten minutes are over: sign in again."])
      expect(codeField().exists()).toBe(false)
      // The email stays, the password is asked again: it was never kept here.
      expect(emailField().element.value).toBe(email)
      expect(passwordField().element.value).toBe('')
    })

    it('goes back to the sign-in when the user asks, with no error left', async () => {
      await toCodeStep()
      await w().find('.back').trigger('click')
      await flushPromises()
      expect(codeField().exists()).toBe(false)
      expect(emailField().element.value).toBe(email)
      expect(summary()).toEqual([])
    })
  })

  it.each([
    [
      new ApiError(400, 'credential_refused', ''),
      "Renault refused the email or the password. If they are right, Renault may have changed its app's key: try again later, or read the guide.",
      '.password',
    ],
    [
      new ApiError(409, 'no_vehicle', ''),
      'This account has no electric vehicle: add the car in the My Renault app first. Nothing was saved.',
    ],
    [
      new ApiError(409, 'too_many_vehicles', ''),
      'With these cars, your account would have more vehicles than it may: the sign-in was not saved.',
    ],
    [
      new ApiError(403, 'not_admitted', ''),
      'Your account must take a step first, such as accepting terms: reload the page. The sign-in was not saved.',
    ],
    [
      new ApiError(403, 'feature_unavailable', ''),
      'Renault is not offered on this instance: the sign-in was not saved.',
    ],
    [
      new ApiError(429, 'rate_limited', ''),
      'Too many sign-ins are waiting for their code on this instance: try again in a few minutes.',
    ],
    [
      new ApiError(502, 'source_unavailable', ''),
      'My Renault did not answer: nothing was saved. Try again in a moment.',
    ],
    [new ApiError(0, 'network', ''), 'Runsten could not be reached. Try again in a moment.'],
    [new ApiError(500, 'internal', ''), 'The sign-in could not be saved. Try again in a moment.'],
  ] as [ApiError, string, string?][])('tells %s', async (e, text, field) => {
    api.signInRenault.mockRejectedValue(e)
    form(none)
    await fill()
    await submit()
    expect(summary()).toEqual([text])
    if (field) expect(w().find(`${field} .v-messages`).text()).toBe(text)
  })

  it('removes the sign-in once confirmed, the vehicles kept, the focus on the next one', async () => {
    api.deleteRenault.mockResolvedValue(undefined)
    form(given)
    await w().find('.delete-connection').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Remove your My Renault sign-in?')
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(api.deleteRenault).toHaveBeenCalled()
    props.connection = none
    await flushPromises()
    expect(status()).toBe('Sign-in removed.')
    expect(document.activeElement).toBe(emailField().element)
  })

  it('removed while a new sign-in awaits its code, goes back to the first step', async () => {
    api.signInRenault.mockResolvedValue(codeSent)
    api.deleteRenault.mockResolvedValue(undefined)
    form(given)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    await fill()
    await submit()
    expect(codeField().exists()).toBe(true)
    await w().find('.delete-connection').trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    props.connection = none
    await flushPromises()
    expect(codeField().exists()).toBe(false)
    expect(document.activeElement).toBe(emailField().element)
    expect((emailField().element as HTMLInputElement).value).toBe('')
  })

  it('takes no sign-in while the source is off, one given before still removable', () => {
    form(given, { off: true })
    expect(w().find('.toggle').exists()).toBe(false)
    expect(w().find('form').exists()).toBe(false)
    expect(w().find('.delete-connection').exists()).toBe(true)
  })
})
