import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings } from '@/entities/connection'
import { BmwForm } from '@/features/manage-bmw'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import device from '@fixtures/bmw-authorization.json'
import connection from '@fixtures/connection.json'
import connected from '@fixtures/connection-bmw.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  startBmwAuthorization: vi.fn(),
  pollBmwToken: vi.fn(),
  deleteBmw: vi.fn(),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

const none: ConnectionSettings = { ...(connection as ConnectionSettings), bmw: null }
const given = connected as ConnectionSettings
const clientId = '4c1cb0a0-1b0e-4d1e-9a3b-0f6f9c3d5e7a'
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  api.startBmwAuthorization.mockResolvedValue(device)
  i18n.global.locale.value = 'en'
  // The code of the golden file expires at 05:05; BMW's interval is 5 s.
  vi.useFakeTimers({
    now: Date.parse('2026-09-28T05:00:00Z'),
    toFake: ['Date', 'setTimeout', 'clearTimeout'],
  })
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
  wrapper = mountWith(BmwForm, { props, attachTo: document.body }).wrapper
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
async function wait(ms: number) {
  await vi.advanceTimersByTimeAsync(ms)
  await flushPromises()
}
async function sendClientId() {
  await w().find('#bmw-client-id').setValue(` ${clientId} `)
  await submit()
}
const summary = () =>
  w()
    .findAll('.error-summary li')
    .map((li) => li.text())
const status = () => w().find('[role="status"]').text()

describe('BmwForm', () => {
  it('without a connection, is open on the steps in the portal, the field and the guide', () => {
    form(none)
    expect(w().text()).toContain('Runsten reads your BMW or MINI through BMW CarData')
    expect(w().text()).not.toContain('beta')
    expect(w().find('a.guide').attributes('href')).toBe('https://runsten.app/docs/guide/bmw')
    const steps = w()
      .findAll('.steps li')
      .map((li) => li.text())
    expect(steps).toHaveLength(4)
    expect(steps[0]).toContain('“BMW CarData”')
    expect(steps[0]).toContain('“Create CarData Client”')
    expect(steps[1]).toContain('one at a time')
    expect(steps[3]).toContain('Never click “Authenticate device”')
    expect(w().find('#bmw-client-id').exists()).toBe(true)
    expect(w().find('.cancel').exists()).toBe(false)
    expect(w().find('.approval').exists()).toBe(false)
    // The status line is always in the page, empty.
    expect(status()).toBe('')
  })

  it('tells what is wrong with the client ID before sending, then as it changes', async () => {
    form(none)
    await submit()
    expect(api.startBmwAuthorization).not.toHaveBeenCalled()
    expect(summary()).toEqual(['Paste the client ID.'])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    const field = w().find('#bmw-client-id')
    expect(field.attributes('aria-invalid')).toBe('true')
    await field.setValue('abc')
    expect(summary()).toEqual(['The client ID has at least 8 characters: copy it whole.'])
    await field.setValue('abcdefgh ijkl')
    expect(summary()).toEqual(['The client ID has no space: copy it again.'])
    await field.setValue('abcdéfghijkl')
    expect(summary()).toEqual(['The client ID has only letters, digits and signs: copy it again.'])
    await field.setValue('a'.repeat(129))
    expect(summary()).toEqual(['The client ID has at most 128 characters: copy it alone.'])
  })

  it("shows BMW's code, its link and its expiry, then connects once the owner approved it", async () => {
    api.pollBmwToken.mockRejectedValueOnce(new ApiError(409, 'authorization_pending', '', 5))
    // The connection is stored before the wait ends: the query gives it to the form.
    api.pollBmwToken.mockImplementation(() => {
      props.connection = given
      return Promise.resolve(given)
    })
    form(none)
    await sendClientId()
    expect(api.startBmwAuthorization).toHaveBeenCalledWith(clientId)
    expect(w().find('.user-code').text()).toBe('ABCD-EFGH')
    const link = w().find('a.bmw-page')
    expect(link.attributes('href')).toBe('https://customer.bmwgroup.com/oneid/link')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noreferrer')
    expect(w().find('.expires').text()).toBe('Valid until 05:05.')
    // The field gives way to the code, whose link takes the focus; the wait is told.
    expect(w().find('#bmw-client-id').exists()).toBe(false)
    expect(document.activeElement).toBe(link.element)
    expect(status()).toContain('Waiting for you to approve the code')

    await wait(5_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(1)
    expect(w().find('.approval').exists()).toBe(true)
    await wait(5_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(2)
    expect(status()).toBe('BMW connected: Runsten reads your cars from its next pass.')
    expect(w().find('form').exists()).toBe(false)
    expect(w().find('.approval').exists()).toBe(false)
    expect(document.activeElement).toBe(w().find('.toggle').element)
  })

  it('opens the page with the code filled in when BMW gives one', async () => {
    api.startBmwAuthorization.mockResolvedValue({
      ...device,
      verification_uri_complete: 'https://customer.bmwgroup.com/oneid/link?user_code=ABCD-EFGH',
    })
    form(none)
    await sendClientId()
    expect(w().find('a.bmw-page').attributes('href')).toBe(
      'https://customer.bmwgroup.com/oneid/link?user_code=ABCD-EFGH',
    )
  })

  it('tells BMW did not answer while it keeps asking', async () => {
    api.pollBmwToken.mockRejectedValue(new ApiError(502, 'source_unavailable', ''))
    form(none)
    await sendClientId()
    await wait(5_000)
    expect(status()).toBe(
      'BMW did not answer: Runsten keeps trying, the code may still be approved.',
    )
    expect(w().find('.approval').exists()).toBe(true)
  })

  it('asks BMW nothing once left while the code was being asked for', async () => {
    let give: (d: typeof device) => void = () => {}
    api.startBmwAuthorization.mockReturnValue(new Promise((resolve) => (give = resolve)))
    form(none)
    await sendClientId()
    w().unmount()
    wrapper = undefined
    give(device)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(api.pollBmwToken).not.toHaveBeenCalled()
  })

  it('cancels the wait, back to the field with what was typed, and asks BMW no more', async () => {
    api.pollBmwToken.mockRejectedValue(new ApiError(409, 'authorization_pending', '', 5))
    form(none)
    await sendClientId()
    await w().find('.cancel-code').trigger('click')
    await flushPromises()
    expect(w().find('.approval').exists()).toBe(false)
    expect(w().find<HTMLInputElement>('#bmw-client-id').element.value).toBe(` ${clientId} `)
    expect(document.activeElement).toBe(w().find('#bmw-client-id').element)
    expect(status()).toBe('Connection cancelled.')
    await wait(60_000)
    expect(api.pollBmwToken).not.toHaveBeenCalled()
  })

  it.each([
    [new ApiError(400, 'credential_refused', ''), 'BMW does not know this client ID'],
    [new ApiError(400, 'invalid_body', ''), 'This client ID was refused: copy it again.'],
    [new ApiError(403, 'feature_unavailable', ''), 'BMW is not offered on this instance.'],
    [new ApiError(403, 'not_admitted', ''), 'Your account must take a step first'],
    [new ApiError(502, 'source_unavailable', ''), 'BMW did not answer: nothing was connected.'],
    [new ApiError(503, 'source_unavailable', ''), 'Too many connections to BMW are pending'],
    [new ApiError(502, 'unavailable', ''), 'Runsten could not be reached.'],
    [new ApiError(500, 'internal', ''), 'BMW could not be connected.'],
  ])('tells %s when the code is asked for', async (e, text) => {
    api.startBmwAuthorization.mockRejectedValue(e)
    form(none)
    await sendClientId()
    expect(summary()).toHaveLength(1)
    expect(summary()[0]).toContain(text)
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(w().find('.approval').exists()).toBe(false)
  })

  it('leads the summary of a client ID refused to its field', async () => {
    api.startBmwAuthorization.mockRejectedValue(new ApiError(400, 'credential_refused', ''))
    form(none)
    await sendClientId()
    await w().find('.error-summary a').trigger('click')
    expect(document.activeElement).toBe(w().find('#bmw-client-id').element)
    expect(w().find('#bmw-client-id').attributes('aria-invalid')).toBe('true')
  })

  it.each([
    [new ApiError(400, 'authorization_expired', ''), 'The code expired, or none was waiting'],
    [new ApiError(400, 'credential_refused', ''), 'BMW refused the connection'],
    [new ApiError(409, 'scope_missing', ''), 'not subscribed to CarData API'],
    [new ApiError(409, 'no_vehicle', ''), 'the main user of no vehicle'],
    [new ApiError(409, 'too_many_vehicles', ''), 'more vehicles than it may'],
    [new ApiError(403, 'feature_unavailable', ''), 'BMW is not offered on this instance.'],
    [new ApiError(500, 'internal', ''), 'BMW could not be connected.'],
  ])('tells %s once the owner approved, and asks again for a code', async (e, text) => {
    api.pollBmwToken.mockRejectedValue(e)
    form(none)
    await sendClientId()
    await wait(5_000)
    expect(summary()).toHaveLength(1)
    expect(summary()[0]).toContain(text)
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    // Back to the form, the code gone, the client ID kept.
    expect(w().find('.approval').exists()).toBe(false)
    expect(w().find<HTMLInputElement>('#bmw-client-id').element.value).toBe(` ${clientId} `)
    expect(w().find('.error-summary a').exists()).toBe(false)
    await wait(60_000)
    expect(api.pollBmwToken).toHaveBeenCalledTimes(1)
  })

  it('tells a code that expired while BMW did not answer', async () => {
    api.pollBmwToken.mockRejectedValue(new ApiError(502, 'source_unavailable', ''))
    form(none)
    await sendClientId()
    await wait(300_000)
    expect(summary()).toEqual(['The code expired, or none was waiting: get a new code.'])
  })

  it('with a connection, tells its last four characters, connects again behind a button, and is removed', async () => {
    api.deleteBmw.mockResolvedValue(undefined)
    form(given)
    const facts = w()
      .findAll('.fact')
      .map((f) => [f.find('dt').text(), f.find('dd').text()])
    expect(facts).toEqual([
      ['Your client ID', 'Ending in c1d0'],
      ['Connected', '05:00'],
    ])
    expect(w().find('form').exists()).toBe(false)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(true)
    expect(document.activeElement).toBe(w().find('#bmw-client-id').element)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(w().find('.toggle').element)

    await w().find('.delete-connection').trigger('click')
    await flushPromises()
    await vi.waitFor(() => expect(document.querySelector('.confirm-delete')).not.toBeNull())
    expect(document.querySelector('#delete-bmw-title')?.textContent).toContain(
      'Remove your BMW connection?',
    )
    ;(document.querySelector('.confirm-delete') as HTMLElement).click()
    await flushPromises()
    expect(api.deleteBmw).toHaveBeenCalled()
    // The form opens at once for the next one, with its field focused.
    expect(w().find('form').exists()).toBe(true)
    await vi.waitFor(() => expect(status()).toBe('BMW connection removed.'))
    expect(document.activeElement).toBe(w().find('#bmw-client-id').element)
  })

  it('tells a removal that failed, in the dialog', async () => {
    api.deleteBmw.mockRejectedValue(new ApiError(500, 'internal', ''))
    form(given)
    await w().find('.delete-connection').trigger('click')
    await flushPromises()
    await vi.waitFor(() => expect(document.querySelector('.confirm-delete')).not.toBeNull())
    ;(document.querySelector('.confirm-delete') as HTMLElement).click()
    await flushPromises()
    await vi.waitFor(() =>
      expect(document.querySelector('.v-dialog .v-alert')?.textContent).toContain(
        'The connection could not be removed',
      ),
    )
  })

  it('tells a connection refused, and leads to connect again', () => {
    form({
      ...given,
      bmw: given.bmw && { ...given.bmw, refused_at: '2026-09-28T04:00:00Z' },
    })
    expect(w().find('.refused .v-alert-title').text()).toBe('Your BMW connection was refused')
    expect(w().find('.refused').text()).toContain('04:00')
  })
})
