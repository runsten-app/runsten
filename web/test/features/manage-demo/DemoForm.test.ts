import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings } from '@/entities/connection'
import { DemoForm } from '@/features/manage-demo'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({ connectDemo: vi.fn(), deleteDemo: vi.fn() }))
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
  ford: null,
  ford_application: { redirect_uri: 'http://localhost/auth/ford/callback', callback: false },
  demo: null,
}
const given: ConnectionSettings = { ...none, demo: { set_at: '2026-09-28T05:00:00Z' } }
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

// props is reactive: the connection the API gives back replaces it, as the query does.
let props = reactive<{ connection: ConnectionSettings; off?: boolean }>({ connection: none })
function form(c: ConnectionSettings, extra: { off?: boolean } = {}) {
  props = reactive({ connection: c, ...extra })
  wrapper = mountWith(DemoForm, { props, attachTo: document.body }).wrapper
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const status = () => w().find('[role="status"]').text()
const facts = () =>
  w()
    .findAll('.fact')
    .map((f) => [f.find('dt').text(), f.find('dd').text()])

describe('DemoForm', () => {
  it('without a demo vehicle, says what it is and offers to connect it', () => {
    form(none)
    expect(w().text()).toContain('A vehicle that needs no car and no maker’s account')
    expect(w().find('.connect').exists()).toBe(true)
    expect(w().find('.disconnect').exists()).toBe(false)
    expect(w().find('.facts').exists()).toBe(false)
  })

  it('connects it, tells so, and puts the focus on the button that disconnects', async () => {
    api.connectDemo.mockResolvedValue(given)
    form(none)
    await w().find('.connect').trigger('click')
    expect(api.connectDemo).toHaveBeenCalled()
    props.connection = given
    await flushPromises()
    expect(status()).toBe(
      'Demo vehicle connected: its last four weeks are in your vehicles, and it goes on being read.',
    )
    expect(document.activeElement).toBe(w().find('.disconnect').element)
    expect(w().find('.connect').exists()).toBe(false)
  })

  it('tells when it was connected', () => {
    form(given)
    expect(facts()).toEqual([
      ['Status', 'Connected'],
      ['Connected on', '05:00'], // today: the time alone
    ])
  })

  it.each([
    [
      new ApiError(403, 'feature_unavailable', ''),
      'The demo vehicle is not offered on this instance.',
    ],
    [
      new ApiError(409, 'too_many_vehicles', ''),
      'With the demo vehicle, your account would have more vehicles than it may: it was not connected.',
    ],
    [
      new ApiError(403, 'not_admitted', ''),
      'Your account must take a step first, such as accepting terms: reload the page. The demo vehicle was not connected.',
    ],
    [new ApiError(0, 'network', ''), 'Runsten could not be reached. Try again in a moment.'],
    [
      new ApiError(500, 'internal', ''),
      'The demo vehicle could not be connected. Try again in a moment.',
    ],
  ] as [ApiError, string][])('tells %s', async (e, text) => {
    api.connectDemo.mockRejectedValue(e)
    form(none)
    await w().find('.connect').trigger('click')
    await flushPromises()
    expect(w().find('.error-summary h2').text()).toBe('The demo vehicle was not connected')
    expect(
      w()
        .findAll('.error-summary li')
        .map((li) => li.text()),
    ).toEqual([text])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(w().find('.connect').exists()).toBe(true)
  })

  it('deletes it once confirmed, and tells the page, which has the focus from there', async () => {
    api.deleteDemo.mockResolvedValue(undefined)
    form(given)
    await w().find('.disconnect').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Disconnect the demo vehicle?')
    document.querySelector<HTMLButtonElement>('.confirm-disconnect')?.click()
    await flushPromises()
    expect(api.deleteDemo).toHaveBeenCalled()
    expect(w().findComponent(DemoForm).emitted('disconnected')).toHaveLength(1)
    // The dialog's close does not take the focus back to a button that is going.
    expect(document.activeElement).not.toBe(w().find('.disconnect').element)
  })

  it('keeps it when the confirmation is cancelled, the focus back on its button', async () => {
    form(given)
    await w().find('.disconnect').trigger('click')
    await flushPromises()
    const cancel = [...document.querySelectorAll<HTMLButtonElement>('.v-card-actions button')].find(
      (b) => b.textContent?.trim() === 'Cancel',
    )
    cancel?.click()
    await vi.waitFor(() => expect(document.activeElement).toBe(w().find('.disconnect').element))
    expect(api.deleteDemo).not.toHaveBeenCalled()
  })

  it('tells a deletion that failed, in its dialog', async () => {
    api.deleteDemo.mockRejectedValue(new ApiError(500, 'internal', ''))
    form(given)
    await w().find('.disconnect').trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('.confirm-disconnect')?.click()
    await flushPromises()
    expect(document.body.textContent).toContain(
      'The demo vehicle could not be deleted. Try again in a moment.',
    )
  })

  it('off, offers nothing to connect, and still disconnects one connected before', () => {
    form(none, { off: true })
    expect(w().find('.connect').exists()).toBe(false)
    wrapper?.unmount()
    form(given, { off: true })
    expect(w().find('.disconnect').exists()).toBe(true)
  })
})
