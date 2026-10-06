import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { h } from 'vue'
import { SettingsPage } from '@/pages/settings'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import orphans from '@fixtures/orphans.json'
import places from '@fixtures/places.json'
import settings from '@fixtures/settings.json'
import unset from '@fixtures/settings-unset.json'
import vehicles from '@fixtures/vehicles.json'
import mqtt from '@fixtures/mqtt.json'
import mqttNone from '@fixtures/mqtt-none.json'
import { sessionKeys } from '@/entities/session'
import session from '@fixtures/session.json'
import { mountWith, testQueryClient } from '@test/utils'

const mocks = vi.hoisted(() => ({
  fetchSettings: vi.fn(),
  setCurrency: vi.fn(),
  fetchPlaces: vi.fn(),
  fetchOrphanCosts: vi.fn(),
  fetchVehicles: vi.fn(),
  fetchAccessTokens: vi.fn(),
  fetchMqtt: vi.fn(),
}))
vi.mock('@/entities/mqtt', async (original) => ({
  ...(await original<typeof import('@/entities/mqtt')>()),
  fetchMqtt: mocks.fetchMqtt,
}))
vi.mock('@/entities/access-token', async (original) => ({
  ...(await original<typeof import('@/entities/access-token')>()),
  fetchAccessTokens: mocks.fetchAccessTokens,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
}))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  fetchSettings: mocks.fetchSettings,
  setCurrency: mocks.setCurrency,
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchOrphanCosts: mocks.fetchOrphanCosts,
}))
vi.mock('@/entities/place', async (original) => ({
  ...(await original<typeof import('@/entities/place')>()),
  fetchPlaces: mocks.fetchPlaces,
}))

// The extensions' sections and account's actions: none in the public build, some in a test.
const { sections, actions } = vi.hoisted(() => ({
  sections: [] as unknown[],
  actions: [] as unknown[],
}))
vi.mock('@/pages/settings/extensions', () => ({
  extensionSections: sections,
  extensionAccountActions: actions,
}))
// The page that lifts the limits: none in the public build.
vi.mock('@/entities/session/extensions', () => ({ limitsPage: null }))

beforeEach(() => {
  sections.length = 0
  actions.length = 0
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchPlaces.mockResolvedValue(places.items)
  mocks.fetchOrphanCosts.mockResolvedValue([])
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
  mocks.fetchAccessTokens.mockResolvedValue([])
  mocks.fetchMqtt.mockResolvedValue(mqttNone)
})

async function page(unavailable: string[] = []) {
  const queryClient = testQueryClient()
  queryClient.setQueryData(sessionKeys.current(), {
    ...session,
    limits: unavailable.length ? { history_from: null, unavailable, unread_vehicles: [] } : null,
  })
  const { wrapper } = mountWith(SettingsPage, { queryClient })
  await flushPromises()
  return wrapper
}

describe('SettingsPage', () => {
  // Only when there is one: most accounts never have any.
  it('has the costs without a charge after the vehicles, when there are some', async () => {
    mocks.fetchSettings.mockResolvedValue(settings)
    mocks.fetchOrphanCosts.mockResolvedValue(orphans.items)
    const w = await page()
    expect(w.findAll('h2').map((h) => h.text())).toEqual([
      'Currency',
      'Places and tariffs',
      'Vehicles',
      'Costs without a charge',
      'API access',
      'MQTT',
      'Your data',
    ])
    expect(w.findAll('.orphan')).toHaveLength(1)
  })

  it("leaves the costs without a charge out, where the account's offer leaves the costs out", async () => {
    mocks.fetchSettings.mockResolvedValue(settings)
    mocks.fetchOrphanCosts.mockResolvedValue(orphans.items)
    const w = await page(['costs'])
    expect(w.findAll('h2').map((h) => h.text())).toEqual([
      'Currency',
      'Places and tariffs',
      'Vehicles',
      'API access',
      'MQTT',
      'Your data',
    ])
    expect(mocks.fetchOrphanCosts).not.toHaveBeenCalled()
  })

  it('has the currency and the places, under their headings', async () => {
    mocks.fetchSettings.mockResolvedValue(settings)
    const w = await page()
    expect(w.find('h1').text()).toBe('Settings')
    expect(w.findAll('h2').map((h) => h.text())).toEqual([
      'Currency',
      'Places and tariffs',
      'Vehicles',
      'API access',
      'MQTT',
      'Your data',
    ])
    expect(w.findComponent({ name: 'VSelect' }).props('modelValue')).toBe('EUR')
    // The prices are in the account's currency.
    expect(w.find('.details').text()).toContain('€0.2142/kWh')
    expect(w.find('.back').exists()).toBe(false)
  })

  // Whatever the offer: the export of the account.
  it("ends with the account's data, to download", async () => {
    mocks.fetchSettings.mockResolvedValue(settings)
    const w = await page(['costs', 'stats'])
    const data = w.find('.account-data')
    expect(data.find('a.export').attributes('href')).toBe('api/v1/account/export')
    expect(data.find('a.export').attributes()).toHaveProperty('download')
  })

  it("has the extensions' actions beside the download of the account's data", async () => {
    mocks.fetchSettings.mockResolvedValue(settings)
    actions.push({ render: () => h('button', { class: 'extension-action' }, 'Delete') })
    const w = await page()
    expect(w.find('.account-data .extension-action').text()).toBe('Delete')
  })

  it("has the extensions' sections after the access tokens, before the account's data", async () => {
    mocks.fetchSettings.mockResolvedValue(settings)
    sections.push({ render: () => h('section', { class: 'extension-section' }, 'Assistants') })
    const w = await page()
    const order = w
      .findAll('section')
      .map((s) => s.attributes('aria-labelledby') ?? s.classes().join(' '))
    const ext = order.indexOf('extension-section')
    expect(ext).toBe(order.indexOf('settings-mqtt-title') + 1)
    expect(order[ext + 1]).toBe('settings-account-title')
  })

  it("locks MQTT where the account's offer leaves it out, the broker set before kept", async () => {
    mocks.fetchSettings.mockResolvedValue(settings)
    mocks.fetchMqtt.mockResolvedValue(mqtt)
    const w = await page(['mqtt'])
    const section = w.find('[aria-labelledby="settings-mqtt-title"]')
    // Read from the messages: an instance may word them otherwise.
    expect(section.find('.feature-unavailable').text()).toContain(i18n.global.t('limits.mqtt'))
    expect(section.text()).toContain('mqtt://homeassistant.local:1883')
    expect(section.find('.toggle').exists()).toBe(false)
    expect(section.find('.delete-broker').exists()).toBe(true)
    expect(section.find('form').exists()).toBe(false)
  })

  it('says costs need a currency, and explains a refused change', async () => {
    mocks.fetchSettings.mockResolvedValue(unset)
    mocks.setCurrency.mockRejectedValue(new ApiError(409, 'currency_in_use', ''))
    const w = await page()
    expect(w.text()).toContain('the costs of charges cannot be computed')
    w.findComponent({ name: 'VSelect' }).vm.$emit('update:modelValue', 'SEK')
    await w.find('.currency-form').trigger('submit')
    await flushPromises()
    expect(w.find('.currency-form .v-alert').text()).toContain('in another currency')
  })

  // A vehicle's model is its own: the settings only lead to it.
  it('leads to the settings of each vehicle, under its label', async () => {
    mocks.fetchSettings.mockResolvedValue(settings)
    const w = await page()
    const links = w.findAll('.vehicle-links a')
    expect(links.map((l) => l.find('.v-list-item-title').text())).toEqual([
      'XC40 Recharge Twin · 2021',
      'YV1SMLT0000DT0002',
      'EX30 Single Motor Extended Range · 2024',
    ])
    expect(links.map((l) => l.attributes('href'))).toEqual(
      vehicles.items.map((v) => `/vehicles/${v.id}/settings`),
    )
    expect(w.find('.vehicle-model').exists()).toBe(false)
    expect(w.find('a.new-place').attributes('href')).toBe('/settings/places/new')
  })

  it.each([
    [
      'no vehicle',
      () => mocks.fetchVehicles.mockResolvedValue([]),
      'No vehicle yet: connect your Volvo ID first.',
    ],
    [
      'a failure',
      () => mocks.fetchVehicles.mockRejectedValue(new ApiError(500, 'internal', '')),
      'The vehicles could not be loaded. Try again in a moment.',
    ],
  ])('tells %s in the vehicles section', async (_, set, text) => {
    mocks.fetchSettings.mockResolvedValue(settings)
    set()
    const w = await page()
    expect(w.find('[aria-labelledby="settings-vehicles-title"]').text()).toContain(text)
  })

  it('tells a failure of the settings', async () => {
    mocks.fetchSettings.mockRejectedValue(new ApiError(500, 'internal', ''))
    const w = await page()
    expect(w.findAll('.v-alert').map((a) => a.text())).toEqual([
      'The settings could not be loaded. Try again in a moment.',
      'The places could not be loaded. Try again in a moment.',
    ])
  })

  // The list tells the limit of the places, from the settings: never a wrong one.
  it('lists the places once the settings are there', async () => {
    mocks.fetchSettings.mockReturnValue(new Promise(() => {}))
    const w = await page()
    expect(w.find('.place-list').exists()).toBe(false)
    expect(w.find('.new-place').exists()).toBe(false)
    // Every field holds an idle loader of its own: only the sections' count.
    const loading = w.findAll('[role="progressbar"]').filter((p) => !p.element.closest('.v-field'))
    expect(loading.map((p) => p.attributes('aria-label'))).toEqual([
      'Loading the settings',
      'Loading the places',
    ])
  })
})
