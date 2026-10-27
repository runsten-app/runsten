import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings } from '@/entities/connection'
import { TeslaForm } from '@/features/manage-tesla'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import connection from '@fixtures/connection.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  setTeslaToken: vi.fn(),
  deleteTeslaToken: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

const none: ConnectionSettings = { ...(connection as ConnectionSettings), tesla: null }
const given: ConnectionSettings = {
  ...none,
  tesla: { last4: 'Qx9z', set_at: '2026-09-28T05:00:00Z', refused_at: null },
}
const token = 'eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.refresh.Qx9z'
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
  wrapper = mountWith(TeslaForm, { props, attachTo: document.body }).wrapper
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const input = () => w().find<HTMLInputElement>('#tesla-refresh-token')
async function submit() {
  await w().find('form').trigger('submit')
  await flushPromises()
}
const summary = () =>
  w()
    .findAll('.error-summary li')
    .map((li) => li.text())
const status = () => w().find('[role="status"]').text()

describe('TeslaForm', () => {
  it('without a token, is open with the steps to Tesla Auth and the guide', () => {
    form(none)
    expect(w().text()).toContain(
      'Runsten reads your Tesla with a refresh token of your Tesla account',
    )
    expect(w().text()).toContain('It never asks for your Tesla password')
    expect(w().text()).toContain('Tesla support is in beta')
    const tool = w().find('a.tool')
    expect(tool.attributes('href')).toBe('https://github.com/adriankumpf/tesla_auth')
    expect(tool.attributes('rel')).toBe('noreferrer')
    expect(w().find('a.guide').attributes('href')).toBe('https://runsten.app/docs/guide/tesla')
    expect(w().findAll('.steps li')).toHaveLength(3)
    // Typed as a password: neither shown nor kept by the browser's autofill.
    expect(input().attributes('type')).toBe('password')
    expect(input().attributes('autocomplete')).toBe('new-password')
    expect(w().find('.cancel').exists()).toBe(false)
  })

  it('with a token, tells it by its last four characters, replaced behind a button', async () => {
    form(given)
    const facts = w()
      .findAll('.fact')
      .map((f) => [f.find('dt').text(), f.find('dd').text()])
    expect(facts).toEqual([
      ['Your refresh token', 'Ending in Qx9z'],
      ['Given', '05:00'],
    ])
    expect(w().find('.refused').exists()).toBe(false)
    expect(w().find('form').exists()).toBe(false)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(input().element)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(w().find('.toggle').element)
  })

  it('tells a token refused', () => {
    form({ ...given, tesla: given.tesla && { ...given.tesla, refused_at: '2026-09-28T06:00:00Z' } })
    expect(w().find('.refused .v-alert-title').text()).toBe('Your Tesla refresh token was refused')
    expect(w().find('.refused').text()).toContain('06:00')
    expect(w().find('.toggle').classes()).toContain('text-primary')
  })

  it('tells what is wrong before sending', async () => {
    form(none)
    await submit()
    expect(api.setTeslaToken).not.toHaveBeenCalled()
    expect(summary()).toEqual(['Paste the refresh token.'])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(input().attributes('aria-invalid')).toBe('true')
    await input().setValue('eyJhbGci OiJSUzI1NiIsInR5cCI6')
    expect(summary()).toEqual(['The refresh token has no space: copy it again.'])
  })

  it('saves the token, trimmed, and says so', async () => {
    api.setTeslaToken.mockResolvedValue(given)
    form(none)
    await input().setValue(` ${token} `)
    await submit()
    expect(api.setTeslaToken).toHaveBeenCalledWith(token)
    props.connection = given
    await flushPromises()
    expect(status()).toBe('Token saved: Runsten reads its vehicles from its next pass.')
    expect(w().find('form').exists()).toBe(false)
    expect(w().text()).not.toContain(token)
  })

  it('tells a token Tesla refused, under its field', async () => {
    api.setTeslaToken.mockRejectedValue(new ApiError(400, 'credential_refused', 'refused'))
    form(none)
    await input().setValue(token)
    await submit()
    const refused =
      'Tesla refused this refresh token: get a new one, it may have been used or revoked.'
    expect(summary()).toEqual([refused])
    expect(w().find('.value .v-messages').text()).toBe(refused)
    await input().setValue(`${token}2`)
    expect(summary()).toEqual([])
  })

  it.each([
    [
      new ApiError(409, 'api_closed', ''),
      "Tesla's Owner API is closed to this account: Runsten cannot read it. The token was not saved.",
    ],
    [
      new ApiError(409, 'no_vehicle', ''),
      'This Tesla account has no vehicle: the token was not saved.',
    ],
    [
      new ApiError(409, 'too_many_vehicles', ''),
      "With this account's vehicles, your account would have more vehicles than it may: the token was not saved.",
    ],
    [
      new ApiError(403, 'not_admitted', ''),
      'Your account must take a step first, such as accepting terms: reload the page. The token was not saved.',
    ],
    [
      new ApiError(502, 'source_unavailable', ''),
      'Tesla did not answer: the token was not saved. Try again in a moment, perhaps with a new token.',
    ],
    [new ApiError(400, 'invalid_body', ''), 'This token was refused: copy it again.'],
    [new ApiError(0, 'network', ''), 'Runsten could not be reached. Try again in a moment.'],
    [new ApiError(500, 'internal', ''), 'The token could not be saved. Try again in a moment.'],
  ])('tells %s', async (e, text) => {
    api.setTeslaToken.mockRejectedValue(e)
    form(none)
    await input().setValue(token)
    await submit()
    expect(summary()).toEqual([text])
  })

  it('removes the token once confirmed, the vehicles kept, the focus on the next one', async () => {
    api.deleteTeslaToken.mockResolvedValue(undefined)
    form(given)
    await w().find('.delete-token').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Remove your Tesla refresh token?')
    expect(document.body.textContent).toContain('Your Tesla vehicles stay, with their history')
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(api.deleteTeslaToken).toHaveBeenCalled()
    props.connection = none
    await flushPromises()
    expect(status()).toBe('Token removed.')
    expect(document.activeElement).toBe(input().element)
  })

  it('keeps the dialog open on a failed removal', async () => {
    api.deleteTeslaToken.mockRejectedValue(new ApiError(500, 'internal', ''))
    form(given)
    await w().find('.delete-token').trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(document.body.textContent).toContain('The token could not be removed.')
  })

  it('is written in French and Swedish too', () => {
    i18n.global.locale.value = 'fr'
    form(none)
    expect(w().find('h3').text()).toBe('Obtenir votre jeton de rafraîchissement')
    wrapper?.unmount()
    i18n.global.locale.value = 'sv'
    form(none)
    expect(w().find('h3').text()).toBe('Hämta din uppdateringstoken')
  })
})
