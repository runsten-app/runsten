import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MqttSettings } from '@/entities/mqtt'
import { MqttBroker } from '@/features/manage-mqtt'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import mqtt from '@fixtures/mqtt.json'
import none from '@fixtures/mqtt-none.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchMqtt: vi.fn(),
  setMqttBroker: vi.fn(),
  deleteMqttBroker: vi.fn(),
}))
vi.mock('@/entities/mqtt', async (original) => ({
  ...(await original<typeof import('@/entities/mqtt')>()),
  ...api,
}))

const withBroker = mqtt as MqttSettings
const empty = none as MqttSettings
const connected: MqttSettings = {
  ...withBroker,
  status: { connected_at: '2026-09-28T06:30:00Z', failed_at: null, failure: null },
}
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

async function mounted(settings: MqttSettings, locked = false) {
  api.fetchMqtt.mockResolvedValue(settings)
  wrapper = mountWith(MqttBroker, { props: { locked }, attachTo: document.body }).wrapper
  await flushPromises()
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const input = (id: string) => w().find<HTMLInputElement>(`#${id}`)
async function submit() {
  await w().find('form').trigger('submit')
  await flushPromises()
}
const summary = () =>
  w()
    .findAll('.error-summary li')
    .map((li) => li.text())
const status = () => w().find('[role="status"]').text()

describe('MqttBroker', () => {
  it('without a broker, is open with the defaults', async () => {
    await mounted(empty)
    expect(w().text()).toContain('Home Assistant creates its entities by itself')
    expect(input('mqtt-topic-prefix').element.value).toBe('runsten')
    expect(input('mqtt-discovery-prefix').element.value).toBe('homeassistant')
    expect(w().find('.cancel').exists()).toBe(false)
    expect(w().text()).toContain('mqtt://homeassistant.local:1883')
  })

  it('tells what is wrong before sending, each line leading to its field', async () => {
    await mounted(empty)
    await input('mqtt-topic-prefix').setValue('runsten/#')
    await submit()
    expect(api.setMqttBroker).not.toHaveBeenCalled()
    expect(summary()).toEqual([
      'Broker URL: Fill it in.',
      'Topic prefix: Topic levels separated by /, without + or #, nor a / at either end.',
    ])
    expect(document.activeElement).toBe(w().find('.error-summary').element)
    expect(input('mqtt-url').attributes('aria-invalid')).toBe('true')
    // The advanced fields open to show their issue.
    expect(w().find<HTMLDetailsElement>('details').element.open).toBe(true)
  })

  it('saves a broker, and says so', async () => {
    api.setMqttBroker.mockResolvedValue(withBroker)
    await mounted(empty)
    await input('mqtt-url').setValue('mqtt://homeassistant.local:1883')
    await input('mqtt-username').setValue('runsten')
    await input('mqtt-password').setValue('s3cret')
    await submit()
    expect(api.setMqttBroker).toHaveBeenCalledWith({
      url: 'mqtt://homeassistant.local:1883',
      username: 'runsten',
      password: 's3cret',
      client_id: '',
      topic_prefix: 'runsten',
      discovery: true,
      discovery_prefix: 'homeassistant',
      publish_location: true,
    })
    expect(status()).toBe('Broker saved: Runsten connects to it within a minute.')
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(w().find('.toggle').element)
  })

  it('shows a broker without its password, and the failure of the connection', async () => {
    await mounted(withBroker)
    expect(w().find('form').exists()).toBe(false)
    expect(w().text()).toContain('mqtt://homeassistant.local:1883')
    expect(w().text()).toContain('Set')
    expect(w().text()).toContain('On, under homeassistant')
    expect(w().find('.failure .v-alert-title').text()).toBe('Runsten cannot publish to your broker')
    expect(w().find('.failure').text()).toContain('refused the username or the password')
  })

  it('tells a connection that holds, or one still to come', async () => {
    await mounted(connected)
    expect(w().find('.failure').exists()).toBe(false)
    expect(w().find('.connected').text()).toContain('Connected since')
    wrapper?.unmount()
    await mounted({ ...withBroker, status: { connected_at: null, failed_at: null, failure: null } })
    expect(w().find('.waiting').text()).toBe('Not connected yet: Runsten connects within a minute.')
  })

  it('changes the broker behind a button, keeping the password for the same host', async () => {
    api.setMqttBroker.mockResolvedValue(withBroker)
    await mounted(withBroker)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(input('mqtt-url').element)
    expect(input('mqtt-url').element.value).toBe('mqtt://homeassistant.local:1883')
    expect(input('mqtt-password').element.value).toBe('')
    await w().find('.remove-password input').setValue(false)
    await submit()
    expect(api.setMqttBroker).toHaveBeenCalledWith(expect.objectContaining({ password: null }))
  })

  it('asks the password again for another host', async () => {
    await mounted(withBroker)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    await input('mqtt-url').setValue('mqtt://elsewhere.example')
    await submit()
    expect(api.setMqttBroker).not.toHaveBeenCalled()
    expect(summary()).toEqual(['Password: Give the password again for another host, or remove it.'])
    await w().find('.remove-password input').setValue(true)
    expect(input('mqtt-password').attributes('disabled')).toBeDefined()
    api.setMqttBroker.mockResolvedValue(withBroker)
    await submit()
    expect(api.setMqttBroker).toHaveBeenCalledWith(expect.objectContaining({ password: '' }))
  })

  it('cancels a change, the focus back on its button', async () => {
    await mounted(withBroker)
    await w().find('.toggle').trigger('click')
    await flushPromises()
    await w().find('.cancel').trigger('click')
    await flushPromises()
    expect(w().find('form').exists()).toBe(false)
    expect(document.activeElement).toBe(w().find('.toggle').element)
  })

  it.each([
    [new ApiError(400, 'broker_refused', ''), 'Runsten does not publish to this address'],
    [new ApiError(400, 'invalid_body', ''), 'This broker was refused: check its fields.'],
    [new ApiError(403, 'feature_unavailable', ''), 'Your account does not include MQTT.'],
    [new ApiError(502, 'unavailable', ''), 'Runsten could not be reached.'],
    [new ApiError(500, 'internal', ''), 'The broker could not be saved.'],
  ])('tells a refusal of the server: %s', async (err, text) => {
    api.setMqttBroker.mockRejectedValue(err)
    await mounted(empty)
    await input('mqtt-url').setValue('mqtts://broker.example')
    await submit()
    expect(summary().join(' ')).toContain(text)
    expect(document.activeElement).toBe(w().find('.error-summary').element)
  })

  it('tells TLS before sending where the instance publishes to the Internet only', async () => {
    await mounted({ ...empty, public_only: true })
    expect(w().text()).toContain('Runsten publishes over TLS only')
    await input('mqtt-url').setValue('mqtt://broker.example')
    await submit()
    expect(summary()).toEqual(['Broker URL: Use mqtts://: Runsten publishes over TLS only.'])
  })

  it('removes the broker once confirmed, the focus on the form of the next one', async () => {
    api.deleteMqttBroker.mockResolvedValue(undefined)
    await mounted(withBroker)
    await w().find('.delete-broker').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Remove the MQTT broker?')
    api.fetchMqtt.mockResolvedValue(empty)
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(api.deleteMqttBroker).toHaveBeenCalled()
    expect(status()).toBe('Broker removed.')
    expect(document.activeElement).toBe(input('mqtt-url').element)
  })

  it('keeps the dialog open on a failed removal', async () => {
    api.deleteMqttBroker.mockRejectedValue(new ApiError(500, 'internal', ''))
    await mounted(withBroker)
    await w().find('.delete-broker').trigger('click')
    await flushPromises()
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(document.body.textContent).toContain('The broker could not be removed.')
  })

  it('locked, shows the broker set before, removable, never changed', async () => {
    await mounted(withBroker, true)
    expect(w().find('.toggle').exists()).toBe(false)
    expect(w().find('.delete-broker').exists()).toBe(true)
    wrapper?.unmount()
    await mounted(empty, true)
    expect(w().find('form').exists()).toBe(false)
  })

  it('tells a failure to load the broker', async () => {
    api.fetchMqtt.mockRejectedValue(new ApiError(500, 'internal', ''))
    wrapper = mountWith(MqttBroker, { props: { locked: false } }).wrapper
    await flushPromises()
    expect(w().find('.v-alert').text()).toBe('The MQTT broker could not be loaded.')
  })
})
