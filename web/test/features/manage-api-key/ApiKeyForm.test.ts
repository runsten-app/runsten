import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { ConnectionSettings } from '@/entities/connection'
import { ApiKeyForm } from '@/features/manage-api-key'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import connection from '@fixtures/connection.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({ setApiKey: vi.fn(), deleteApiKey: vi.fn() }))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  ...api,
}))

const key = '0123456789abcdef0123456789abcdef'
const hosted: ConnectionSettings = {
  connected: false,
  client_id: 'runsten-dev',
  instance_key: null,
  api_key: null,
}
const withKey = connection as ConnectionSettings
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
let props = reactive({ connection: hosted })
function form(c: ConnectionSettings) {
  props = reactive({ connection: c })
  wrapper = mountWith(ApiKeyForm, { props, attachTo: document.body }).wrapper
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const field = () => w().find<HTMLInputElement>('#api-key')
async function type(value: string) {
  await field().setValue(value)
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

describe('ApiKeyForm', () => {
  it('without the instance key, is open with the steps to the developer portal', () => {
    form(hosted)
    expect(w().text()).toContain('This instance has none of its own')
    const link = w().find('a.portal')
    expect(link.attributes('href')).toBe('https://developer.volvocars.com/')
    expect(link.attributes('rel')).toBe('noreferrer')
    expect(link.text()).toContain('developer.volvocars.com')
    expect(w().findAll('.steps li')).toHaveLength(5)
    expect(w().text()).toContain('Do not publish it')
    expect(w().find('.cancel').exists()).toBe(false)
  })

  it('with a key, replaces it behind a button', async () => {
    form(withKey)
    expect(w().find('form').exists()).toBe(false)
    expect(w().find('.toggle').text()).toBe('Replace the key')
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(field().element)
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(w().find('.toggle').element)
  })

  it('tells what is wrong before sending, in a summary that takes the focus', async () => {
    form(hosted)
    await submit()
    expect(api.setApiKey).not.toHaveBeenCalled()
    expect(summary()).toEqual(['Paste the key.'])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(field().attributes('aria-invalid')).toBe('true')
    await type('0123 4567')
    expect(summary()).toEqual(['A key has no space: copy it again.'])
  })

  it('saves the key, trimmed, and says so', async () => {
    api.setApiKey.mockResolvedValue(withKey)
    form(hosted)
    await type(`  ${key} `)
    await submit()
    expect(api.setApiKey).toHaveBeenCalledWith(key)
    props.connection = withKey
    await flushPromises()
    expect(status()).toBe('Key saved.')
    expect(w().find('form').exists()).toBe(false)
  })

  it('tells a key Volvo refused, under the field', async () => {
    api.setApiKey.mockRejectedValue(new ApiError(400, 'api_key_refused', 'refused'))
    form(hosted)
    await type(key)
    await submit()
    const refused = 'Volvo refused this key: check that it is the primary key of your application.'
    expect(summary()).toEqual([refused])
    expect(w().find('.v-messages').text()).toBe(refused)
    await type(key + 'x')
    expect(summary()).toEqual([])
  })

  it('tells a Volvo ID with too many vehicles', async () => {
    api.setApiKey.mockRejectedValue(new ApiError(409, 'too_many_vehicles', 'too many'))
    form(hosted)
    await type(key)
    await submit()
    expect(summary()).toEqual([
      'Your Volvo ID gives access to more vehicles than your account may have: the key was not saved.',
    ])
  })

  it('tells a failure of the server', async () => {
    api.setApiKey.mockRejectedValue(new ApiError(502, 'unavailable', ''))
    form(hosted)
    await type(key)
    await submit()
    expect(summary()).toEqual(['Runsten could not be reached. Try again in a moment.'])
  })

  it('shows the key given by its last four characters, and its refusal', () => {
    form({
      ...withKey,
      api_key: withKey.api_key && { ...withKey.api_key, refused_at: '2026-09-28T06:00:00Z' },
    })
    expect(w().text()).toContain('Ending in cdef')
    expect(w().text()).not.toContain(key.slice(0, 8))
    expect(w().find('.refused .v-alert-title').text()).toBe('Your Volvo key was refused')
    expect(w().find('.refused').text()).toContain('06:00')
    expect(w().find('.toggle').text()).toBe('Replace the key')
  })

  it('removes the key once confirmed, the focus on the field of the next one', async () => {
    api.deleteApiKey.mockResolvedValue(undefined)
    form(withKey)
    await w().find('.delete-key').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Remove your Volvo key?')
    expect(document.body.textContent).toContain('no longer be read')
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(api.deleteApiKey).toHaveBeenCalled()
    props.connection = hosted
    await flushPromises()
    expect(status()).toBe('Key removed.')
    expect(document.activeElement).toBe(field().element)
  })

  it('keeps the dialog open on a failed removal', async () => {
    api.deleteApiKey.mockRejectedValue(new ApiError(500, 'internal', ''))
    form(withKey)
    await w().find('.delete-key').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('no longer be read')
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(document.body.textContent).toContain('The key could not be removed.')
  })
})
