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
  connected: true,
  client_id: 'runsten-dev',
  instance_key: { last4: 'e123' },
  api_key: null,
}
// The hosted offer: no instance key, the account gives its own.
const hosted: ConnectionSettings = {
  connected: false,
  client_id: 'runsten-dev',
  instance_key: null,
  api_key: null,
}

const items = vehicles.items as Vehicle[]
const plain = (s: string) => s.replace(/\s+/g, ' ').trim()

beforeEach(() => {
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
  it('has the Volvo ID and the vehicles, under their headings', async () => {
    const w = await page()
    expect(w.find('h1').text()).toBe('Connection')
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
    expect(w.text()).toContain('No Volvo ID connected yet: Runsten reads nothing.')
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
  })

  it.each([null, '/plans'])(
    'warns when the Volvo ID has too many vehicles, leading to the page that lifts the limits: %s',
    async (limitsPage) => {
      extension.limitsPage = limitsPage
      const { wrapper: w } = mountWith(ConnectionPage, {
        props: { outcome: 'too_many_vehicles' },
      })
      await flushPromises()
      expect(w.find('.v-alert').text()).toContain('more vehicles than your account may have')
      const lift = w.find('.v-alert a.lift')
      expect(lift.exists() ? lift.attributes('href') : null).toBe(limitsPage)
    },
  )

  it('ignores an unknown outcome', async () => {
    const w = await page({ outcome: 'forged' })
    expect(w.find('.v-alert').exists()).toBe(false)
  })
})
