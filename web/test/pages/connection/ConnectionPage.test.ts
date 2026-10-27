import { flushPromises, type DOMWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ConnectionSettings } from '@/entities/connection'
import type { Vehicle } from '@/entities/vehicle'
import { ConnectionPage } from '@/pages/connection'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

// The page that lifts the limits: none in the public build, one in a test.
const extension = vi.hoisted(() => ({ limitsPage: null as string | null }))
vi.mock('@/entities/session/extensions', () => ({
  get limitsPage() {
    return extension.limitsPage
  },
}))

// The flag source.tesla, as the session gives it: on unless a test turns it off.
const flag = vi.hoisted(() => ({ tesla: true }))
vi.mock('@/features/view-flags', async () => {
  const { computed } = await import('vue')
  return { useFlag: () => computed(() => flag.tesla) }
})

const mocks = vi.hoisted(() => ({ fetchVehicles: vi.fn(), fetchConnection: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  fetchConnection: mocks.fetchConnection,
}))

// Self-hosting: the instance's key reads the vehicles.
const selfHosted: ConnectionSettings = {
  volvo: true,
  connected: true,
  client_id: 'runsten-dev',
  instance_key: { last4: 'e123' },
  api_key: null,
  polestar: null,
  tesla: null,
}
// The hosted offer: no instance key, the account gives its own.
const hosted: ConnectionSettings = {
  volvo: true,
  connected: false,
  client_id: 'runsten-dev',
  instance_key: null,
  api_key: null,
  polestar: null,
  tesla: null,
}

const items = vehicles.items as Vehicle[]
const plain = (s: string) => s.replace(/\s+/g, ' ').trim()

beforeEach(() => {
  flag.tesla = true
  mocks.fetchVehicles.mockReset()
  mocks.fetchVehicles.mockResolvedValue(items)
  mocks.fetchConnection.mockReset()
  mocks.fetchConnection.mockResolvedValue(selfHosted)
  vi.useFakeTimers({ now: Date.parse('2026-09-28T07:05:00Z'), toFake: ['Date'] })
})
afterEach(() => {
  extension.limitsPage = null
  vi.useRealTimers()
})

async function page(props: { vehicle?: string; outcome?: string } = {}) {
  const { wrapper } = mountWith(ConnectionPage, { props })
  await flushPromises()
  return wrapper
}

// facts reads a <dl> as its labels and values.
function facts(dl: DOMWrapper<Element>) {
  return Object.fromEntries(
    dl.findAll('.fact').map((f) => [f.find('dt').text(), plain(f.find('dd').text())]),
  )
}

describe('ConnectionPage', () => {
  it('has one tab per brand, each with its connection and its vehicles', async () => {
    const w = await page()
    expect(w.find('h1').text()).toBe('Connection')
    // A dot on each tab whose vehicles need the user: a lost grant, a Polestar unshared.
    expect(w.findAll('[role="tab"]').map((t) => plain(t.text()))).toEqual([
      'Volvo needs your attention',
      'Polestar Beta needs your attention',
      'Tesla Beta',
    ])
    expect(w.find('[role="tab"][aria-selected="true"]').text()).toContain('Volvo')
    // Self-hosting: the instance's key reads every vehicle, there is none to give.
    expect(w.findAll('h2').map((h) => h.text())).toEqual([
      'Volvo ID',
      'Volvo application',
      'Vehicles',
    ])
    expect(w.find('.api-key-form').exists()).toBe(false)
    expect(w.findAll('.vehicle-collection h3').map((h) => h.text())).toEqual([
      'XC40 Recharge Twin · 2021',
      'YV1SMLT0000DT0002',
      'EX30 Single Motor Extended Range · 2024',
      'YV1SMLT0000DT0005',
    ])

    await w.findAll('[role="tab"]')[1]?.trigger('click')
    await flushPromises()
    // The Volvo panel, once shown, stays in the page, hidden.
    const panel = w.find('#connection-panel-polestar')
    expect(panel.findAll('h2').map((h) => h.text())).toEqual(['Polestar Data Portal', 'Vehicles'])
    expect(panel.findAll('.vehicle-collection h3').map((h) => h.text())).toEqual([
      'Polestar 2 Long Range Dual Motor / Performance · 2024',
    ])

    // Tesla's, without a vehicle yet: the form to give a refresh token.
    await w.findAll('[role="tab"]')[2]?.trigger('click')
    await flushPromises()
    const tesla = w.find('#connection-panel-tesla')
    expect(tesla.findAll('h2').map((h) => h.text())).toEqual(['Tesla account'])
    expect(tesla.find('#tesla-refresh-token').exists()).toBe(true)
  })

  it('leaves Volvo out on an instance without its application', async () => {
    mocks.fetchConnection.mockResolvedValue({
      ...selfHosted,
      volvo: false,
      client_id: '',
      instance_key: null,
    })
    mocks.fetchVehicles.mockResolvedValue(items.filter((v) => v.connection.provider === 'polestar'))
    const w = await page()
    expect(w.findAll('[role="tab"]').map((t) => plain(t.text()))).toEqual([
      'Polestar Beta needs your attention',
      'Tesla Beta',
    ])
    expect(w.find('#connection-panel-polestar').exists()).toBe(true)
    expect(w.find('a[href="auth/volvo/start"]').exists()).toBe(false)
  })

  it('tells why the Volvos from before are not read, without the application', async () => {
    mocks.fetchConnection.mockResolvedValue({
      ...selfHosted,
      volvo: false,
      client_id: '',
      instance_key: null,
    })
    const w = await page()
    expect(w.findAll('[role="tab"]')).toHaveLength(3)
    const panel = w.find('#connection-panel-volvo')
    expect(panel.findAll('h2').map((h) => h.text())).toEqual(['Volvo not configured', 'Vehicles'])
    expect(panel.text()).toContain('This instance has no Volvo application')
    expect(w.find('a[href="auth/volvo/start"]').exists()).toBe(false)
    expect(w.find('.api-key-form').exists()).toBe(false)
  })

  it('leaves the Tesla tab out while its flag is off', async () => {
    flag.tesla = false
    const w = await page()
    expect(w.findAll('[role="tab"]').map((t) => plain(t.text()))).toEqual([
      'Volvo needs your attention',
      'Polestar Beta needs your attention',
    ])
    expect(w.find('#connection-panel-tesla').exists()).toBe(false)
  })

  it('tells why Teslas from before are not read while its flag is off', async () => {
    flag.tesla = false
    const [car] = items
    if (!car) throw new Error('fixture')
    mocks.fetchVehicles.mockResolvedValue([
      {
        ...car,
        id: 'tesla-1',
        connection: { ...car.connection, provider: 'tesla', credential: 'set' },
      },
    ])
    const w = await page()
    expect(w.find('[role="tab"][aria-selected="true"]').text()).toContain('Tesla')
    const panel = w.find('#connection-panel-tesla')
    expect(panel.find('.tesla-off').text()).toContain('Tesla is not offered on this instance')
    expect(panel.find('#tesla-refresh-token').exists()).toBe(false)
  })

  it('puts no dot on a tab whose vehicles are read', async () => {
    const [car] = items
    if (!car) throw new Error('fixture')
    mocks.fetchVehicles.mockResolvedValue([
      { ...car, collection: { ...car.collection, quota: [] } },
    ])
    const w = await page()
    expect(w.findAll('[role="tab"] .attention')).toHaveLength(0)
  })

  // The vehicles still read: the credential alone needs the user.
  it("puts a dot on Polestar's tab when its credential is to be renewed", async () => {
    const [car] = items
    if (!car) throw new Error('fixture')
    mocks.fetchVehicles.mockResolvedValue([
      { ...car, collection: { ...car.collection, quota: [] } },
    ])
    mocks.fetchConnection.mockResolvedValue({
      ...selfHosted,
      polestar: {
        last4: '0123',
        set_at: '2026-07-01T07:00:00Z',
        expires_at: '2026-09-29T07:00:00Z',
        refused_at: null,
      },
    })
    const w = await page()
    expect(w.findAll('[role="tab"]').map((t) => plain(t.text()))).toEqual([
      'Volvo',
      'Polestar Beta needs your attention',
      'Tesla Beta',
    ])
  })

  it('tells the Volvo ID, and connects it again', async () => {
    const w = await page()
    const section = w.find('[aria-labelledby="connection-volvo-title"]')
    expect(facts(section.find('dl'))).toEqual({
      Status: 'Connected',
      Authorized: '05:00',
      'Authorization renewed': '06:00',
    })
    expect(section.find('a').attributes('href')).toBe('auth/volvo/start')
    expect(section.find('a').text()).toBe('Reconnect the Volvo ID')
  })

  it("tells the instance's application, by its client ID and the end of its key", async () => {
    const w = await page()
    const section = w.find('[aria-labelledby="connection-app-title"]')
    expect(facts(section.find('dl'))).toEqual({
      'Client ID': 'runsten-dev',
      'Application key': 'Ending in e123',
    })
    expect(section.text()).toContain('the secret is never shown')
  })

  it('tells a lost grant, and when', async () => {
    mocks.fetchVehicles.mockResolvedValue([items[1]])
    const w = await page()
    const section = w.find('[aria-labelledby="connection-volvo-title"]')
    expect(facts(section.find('dl'))).toEqual({
      Status: 'Re-authentication required',
      Authorized: '05:00',
      Lost: '06:30',
      Reason: 'refresh token expired',
    })
    expect(section.find('a').classes()).toContain('bg-warning')
  })

  it('tells how each vehicle is read', async () => {
    // Read from the messages: an instance may word them otherwise.
    const passedAt = i18n.global.t('connection.facts.passedAt')
    const w = await page()
    const [car, lost, never] = w.findAll('.vehicle-collection')
    expect(car?.find('.state').text()).toBe(
      'Volvo quota reached until 09:00: battery and charging, position',
    )
    expect(car && facts(car.find('dl'))).toEqual({
      'Polling mode': 'Charging',
      [passedAt]: '5 min ago',
      'Latest successful reading': '06:59',
      'Next reading': 'At the next pass',
      'Quota reached': 'battery and charging until 08:00, position until 09:00',
      'Latest failure': 'Quota reached · 06:40location · HTTP 403',
    })
    expect(car?.find('.failure-where').text()).toBe('location · HTTP 403')
    expect(lost?.find('.state').text()).toBe('Not read: the Volvo ID must be connected again')
    expect(lost && facts(lost.find('dl'))).toMatchObject({
      'Next reading': 'None until the Volvo ID is connected again',
      'Latest failure': 'Volvo ID unreachable · 06:30',
    })
    expect(never?.find('.state').text()).toBe(i18n.global.t('connection.state.waiting'))
    expect(never && facts(never.find('dl'))).toEqual({
      'Polling mode': 'Unknown',
      [passedAt]: 'Never',
      'Latest successful reading': 'Unknown',
      'Next reading': 'Unknown',
      'Latest failure': 'None',
    })
  })

  it('tells how a Polestar is read, and takes its credential', async () => {
    const router = testRouter()
    await router.push('/connection#polestar')
    const { wrapper: w } = mountWith(ConnectionPage, { router })
    await flushPromises()
    const v = w.find('.vehicle-collection')
    expect(v?.find('.state').text()).toBe(
      'Not read: the vehicle is no longer shared with the credential',
    )
    expect(v && facts(v.find('dl'))).toMatchObject({
      'Next reading': 'None: the vehicle is no longer shared with the credential',
      'Latest failure': 'Vehicle no longer shared · 06:00battery · HTTP 403',
    })
    expect(w.find('[aria-labelledby="connection-polestar-title"] .polestar-form').exists()).toBe(
      true,
    )
    // In beta: its tab says so.
    expect(w.find('#connection-tab-polestar .beta').text()).toBe('Beta')
  })

  describe('opened at a tab', () => {
    const selected = (w: { find: (s: string) => DOMWrapper<Element> }) =>
      w.find('[role="tab"][aria-selected="true"]').attributes('id')

    it("opens the hash's tab", async () => {
      mocks.fetchConnection.mockResolvedValue(hosted)
      const router = testRouter()
      await router.push('/connection#polestar')
      const { wrapper } = mountWith(ConnectionPage, { router })
      await flushPromises()
      expect(selected(wrapper)).toBe('connection-tab-polestar')
      expect(wrapper.find('.polestar-form').exists()).toBe(true)
    })

    it('opens the tab of the vehicle it was opened from', async () => {
      const w = await page({ vehicle: '0b5c6c3e-3f0e-4a57-9d3b-2f4f9e8d1a06' })
      expect(selected(w)).toBe('connection-tab-polestar')
    })

    it('opens the tab of the only brand the account has', async () => {
      const polestar = items[4]
      if (!polestar) throw new Error('fixture')
      mocks.fetchVehicles.mockResolvedValue([polestar])
      const w = await page()
      expect(selected(w)).toBe('connection-tab-polestar')
    })

    it('keeps the tab chosen in the hash', async () => {
      const router = testRouter()
      await router.push('/connection?vehicle=v1')
      const { wrapper } = mountWith(ConnectionPage, { router })
      await flushPromises()
      expect(selected(wrapper)).toBe('connection-tab-volvo')
      await wrapper.find('#connection-tab-polestar').trigger('click')
      await vi.waitFor(() => expect(router.currentRoute.value.hash).toBe('#polestar'))
      expect(router.currentRoute.value.query).toEqual({ vehicle: 'v1' })
    })
  })

  it('takes the Volvo ID from a Volvo, whatever comes first', async () => {
    const [car, , , , polestar] = items
    if (!car || !polestar) throw new Error('fixture')
    mocks.fetchVehicles.mockResolvedValue([polestar, car])
    const w = await page()
    const section = w.find('[aria-labelledby="connection-volvo-title"]')
    expect(facts(section.find('dl'))).toMatchObject({ Status: 'Connected', Authorized: '05:00' })
  })

  it('shows a next reading still ahead as its time', async () => {
    vi.setSystemTime(Date.parse('2026-09-28T07:00:30Z'))
    const w = await page()
    const car = w.find('.vehicle-collection')
    expect(facts(car.find('dl'))['Next reading']).toBe('07:01')
  })

  it('offers to connect a Volvo ID without a vehicle', async () => {
    mocks.fetchVehicles.mockResolvedValue([])
    mocks.fetchConnection.mockResolvedValue({ ...selfHosted, connected: false })
    const w = await page()
    expect(w.text()).toContain('No Volvo ID connected yet: Runsten reads no Volvo.')
    expect(w.find('a[href="auth/volvo/start"]').text()).toBe('Connect a Volvo ID')
    expect(w.findAll('h2').map((h) => h.text())).toEqual(['Volvo ID', 'Volvo application'])
  })

  it('asks for the key before the Volvo ID, on an instance without its own', async () => {
    mocks.fetchVehicles.mockResolvedValue([])
    mocks.fetchConnection.mockResolvedValue(hosted)
    const w = await page()
    expect(w.findAll('h2').map((h) => h.text())).toEqual(['Volvo key', 'Volvo ID'])
    expect(w.find('.api-key-form form').exists()).toBe(true)
    expect(w.find('a[href="auth/volvo/start"]').exists()).toBe(false)
    expect(w.find('[aria-labelledby="connection-volvo-title"]').text()).toContain(
      'Give your Volvo key first, above',
    )
  })

  it('connects the Volvo ID once the key is given', async () => {
    mocks.fetchVehicles.mockResolvedValue([])
    mocks.fetchConnection.mockResolvedValue({
      ...hosted,
      api_key: { last4: 'cdef', set_at: '2026-09-28T05:00:00Z', refused_at: null },
    })
    const w = await page()
    expect(w.findAll('h2').map((h) => h.text())).toEqual(['Volvo key', 'Volvo ID'])
    expect(w.find('a[href="auth/volvo/start"]').text()).toBe('Connect a Volvo ID')
  })

  it('tells a key refused at the connection, the Volvo ID kept', async () => {
    mocks.fetchVehicles.mockResolvedValue([])
    mocks.fetchConnection.mockResolvedValue({
      volvo: true,
      connected: true,
      client_id: 'runsten-dev',
      instance_key: null,
      api_key: {
        last4: 'cdef',
        set_at: '2026-09-28T05:00:00Z',
        refused_at: '2026-09-28T05:01:00Z',
      },
    })
    const w = await page()
    expect(w.find('.refused .v-alert-title').text()).toBe('Your Volvo key was refused')
    expect(w.find('[aria-labelledby="connection-volvo-title"]').text()).toContain(
      'Your Volvo ID is connected, but no vehicle is listed yet',
    )
  })

  it('tells a Volvo ID without vehicle, its key accepted', async () => {
    mocks.fetchVehicles.mockResolvedValue([])
    mocks.fetchConnection.mockResolvedValue({
      ...hosted,
      volvo: true,
      connected: true,
      api_key: { last4: 'cdef', set_at: '2026-09-28T05:00:00Z', refused_at: null },
    })
    const w = await page()
    expect(w.find('.refused').exists()).toBe(false)
    const section = w.find('[aria-labelledby="connection-volvo-title"]').text()
    expect(section).toContain('Your Volvo ID is connected, but it has no vehicle')
    expect(section).not.toContain('refused')
    expect(section).toContain('click “Reconnect the Volvo ID”')
  })

  it('tells a vehicle whose key was refused', async () => {
    const [car] = items
    if (!car) throw new Error('fixture')
    mocks.fetchVehicles.mockResolvedValue([
      { ...car, connection: { ...car.connection, api_key: 'refused' } },
    ])
    const w = await page()
    const v = w.find('.vehicle-collection')
    expect(v.find('.state').text()).toBe('Not read: Volvo refused the application key')
  })

  it('tells a failure', async () => {
    mocks.fetchVehicles.mockRejectedValue(new ApiError(502, 'unavailable', ''))
    const w = await page()
    expect(w.find('.v-alert').text()).toContain('The connection could not be loaded.')
  })

  it('tells a failure to read the key', async () => {
    mocks.fetchConnection.mockRejectedValue(new ApiError(502, 'unavailable', ''))
    const w = await page()
    expect(w.find('.v-alert').text()).toContain('The connection could not be loaded.')
  })

  it('leads back to the vehicle it was opened from', async () => {
    const w = await page({ vehicle: 'v1' })
    expect(w.find('a.back').attributes('href')).toBe('/vehicles/v1')
  })

  it('tells the outcome of the Volvo ID flow it comes back from, once', async () => {
    const router = testRouter()
    await router.push('/connection?volvo=connected')
    const { wrapper: w } = mountWith(ConnectionPage, { router, props: { outcome: 'connected' } })
    await flushPromises()
    expect(w.find('.v-alert').text()).toContain('Volvo ID connected')
    await vi.waitFor(() => expect(router.currentRoute.value.query).toEqual({}))
  })

  it('warns when Volvo refused the key at the connection', async () => {
    const w = await page({ outcome: 'key_refused' })
    expect(w.find('.v-alert').text()).toContain('Volvo refused your key')
  })

  it('warns when the Volvo ID has no vehicle', async () => {
    const w = await page({ outcome: 'no_vehicle' })
    expect(w.find('.v-alert').text()).toContain('Your Volvo ID has no vehicle')
    expect(w.find('.v-alert').text()).toContain('click “Reconnect the Volvo ID” again') // the button shown: a connection is in place
  })

  it.each([null, '/plans'])(
    'warns when the Volvo ID has too many vehicles, leading to the page that lifts the limits: %s',
    async (limitsPage) => {
      extension.limitsPage = limitsPage
      const { wrapper: w } = mountWith(ConnectionPage, {
        props: { outcome: 'too_many_vehicles' },
      })
      await flushPromises()
      expect(w.find('.v-alert').text()).toContain(
        'your account would have more vehicles than it may',
      )
      const lift = w.find('.v-alert a.lift')
      expect(lift.exists() ? lift.attributes('href') : null).toBe(limitsPage)
    },
  )

  it('ignores an unknown outcome', async () => {
    const w = await page({ outcome: 'forged' })
    expect(w.find('.v-alert').exists()).toBe(false)
  })
})
