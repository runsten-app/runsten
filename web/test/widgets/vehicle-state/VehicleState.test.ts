import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/shared/api'
import { i18n, type Locale } from '@/shared/i18n'
import { VehicleState } from '@/widgets/vehicle-state'
import state from '@fixtures/state.json'
import unknown from '@fixtures/state-unknown.json'
import vehicle from '@fixtures/vehicle-model.json'
import { mountWith } from '@test/utils'

const { fetchVehicle, fetchVehicleState } = vi.hoisted(() => ({
  fetchVehicle: vi.fn(),
  fetchVehicleState: vi.fn(),
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicle,
  fetchVehicleState,
}))

const noVariant = { ...vehicle, model: { ...vehicle.model, variant: null } }

// Three and a half minutes after the latest reading of the fixture.
const now = Date.parse('2026-09-28T19:06:30Z')

beforeEach(() => {
  fetchVehicleState.mockReset()
  fetchVehicle.mockReset()
  fetchVehicle.mockResolvedValue(noVariant)
  vi.useFakeTimers({ now, toFake: ['Date'] })
})
afterEach(() => {
  vi.useRealTimers()
  i18n.global.locale.value = 'en'
})

const plain = (s: string) => s.replace(/\s+/g, ' ').trim()

async function mounted(body: unknown) {
  fetchVehicleState.mockResolvedValue(body)
  const { wrapper } = mountWith(VehicleState, { props: { vehicle: state.vehicle_id } })
  await flushPromises()
  return wrapper
}

// reading finds the item of a label, and returns its value and details.
function reading(w: Awaited<ReturnType<typeof mounted>>, label: string) {
  const item = w.findAll('.reading').find((r) => r.find('dt').text() === label)
  if (!item) throw new Error(`no reading ${label}`)
  return {
    value: plain(item.find('.value').text()),
    details: plain(item.find('.details').exists() ? item.find('.details').text() : ''),
    stale: item.classes().includes('stale'),
  }
}

// provenance is what a tile says once of its values: when checked and since when.
function provenance(w: Awaited<ReturnType<typeof mounted>>, title: string) {
  const tile = w.findAll('.v-card').find((c) => c.find('h2').text().startsWith(title))
  if (!tile) throw new Error(`no tile ${title}`)
  const own = tile.findAll('.details').filter((d) => !d.element.closest('.reading'))
  return own.map((d) => plain(d.text())).join(' | ')
}

describe('VehicleState', () => {
  it('shows each value with its freshness', async () => {
    const w = await mounted(state)
    expect(plain(w.text())).toContain('Last checked 3 min ago')
    // Said once for the tile, repeated by a value only where it differs.
    expect(provenance(w, 'Battery')).toBe('checked 3 min ago · since 19:03')
    expect(reading(w, 'State of charge')).toEqual({ value: '64%', details: '', stale: false })
    expect(reading(w, 'Range').value).toBe('256 km')
    expect(reading(w, 'Odometer')).toMatchObject({ value: '12,473 km', stale: false })
    expect(reading(w, 'Engine').value).toBe('Stopped')
    // Read once a day: not stale after 14 hours.
    expect(reading(w, 'Battery capacity')).toMatchObject({ value: '78 kWh', stale: false })
    expect(reading(w, 'Battery capacity').details).toMatch(/^checked .+ · since /)
    expect(reading(w, 'Coordinates')).toMatchObject({ value: '45.76400, 4.83570', details: '' })
    expect(provenance(w, 'Last position')).toBe('checked 36 min ago · since 17:30')
  })

  it('puts a charge in progress forward', async () => {
    const w = await mounted(state)
    expect(plain(w.find('.mode').text())).toBe('Charging')
    // The status is the tile's pill, named for a screen reader.
    const status = w.find('.charging-status')
    expect(plain(status.text())).toBe('Status: Charging')
    expect(status.classes()).toContain('status-pill--success')
    expect(reading(w, 'Power').value).toBe('7.4 kW')
    expect(reading(w, 'Type').value).toBe('AC')
    expect(reading(w, 'Cable').value).toBe('Connected')
    expect(reading(w, 'Target').value).toBe('80%')
  })

  it('does not when the car is parked', async () => {
    const parked = {
      ...state,
      charging: { ...state.charging, status: { ...state.charging.status, value: 'done' } },
    }
    const w = await mounted(parked)
    expect(plain(w.find('.mode').text())).toBe('Parked')
    expect(plain(w.find('.charging-status').text())).toBe('Status: Done')
  })

  it('tells a charge type the car says is none from an unknown one', async () => {
    const none = {
      ...state,
      charging: { ...state.charging, type: { ...state.charging.type, value: 'none' } },
    }
    expect(reading(await mounted(none), 'Type').value).toBe('None')
    i18n.global.locale.value = 'fr'
    expect(reading(await mounted(none), 'Type').value).toBe('Aucun')
  })

  it('tells no power where the car says no charge runs, an unknown one during a charge', async () => {
    const idle = {
      ...state,
      charging: {
        ...state.charging,
        power_w: null,
        status: { ...state.charging.status, value: 'idle' },
      },
    }
    expect(reading(await mounted(idle), 'Power').value).toBe('None')
    const charging = { ...state, charging: { ...state.charging, power_w: null } }
    expect(reading(await mounted(charging), 'Power').value).toBe('Unknown')
    i18n.global.locale.value = 'fr'
    expect(reading(await mounted(idle), 'Puissance').value).toBe('Aucune')
  })

  it('links to OpenStreetMap, nowhere else, only on a click', async () => {
    const w = await mounted(state)
    const link = w.find('a[target="_blank"]')
    expect(link.attributes('href')).toBe(
      'https://www.openstreetmap.org/?mlat=45.76400&mlon=4.83570#map=16/45.76400/4.83570',
    )
    expect(link.attributes('rel')).toBe('noreferrer')
    expect(link.text()).toBe('Open in OpenStreetMap')
    expect(w.findAll('img, iframe')).toHaveLength(0)
  })

  it('marks the readings older than two hours as stale', async () => {
    vi.setSystemTime(Date.parse('2026-09-28T21:10:00Z'))
    const w = await mounted(state)
    expect(reading(w, 'State of charge')).toMatchObject({ stale: true })
    expect(provenance(w, 'Battery')).toContain('stale')
    expect(reading(w, 'Battery capacity').stale).toBe(false)
    expect(w.find('time').text()).toBe('Last checked 2 hr ago')
    expect(w.findAll('.v-chip').map((c) => c.text())).toContain('stale')
  })

  it.each<[Locale, string, string]>([
    ['en', 'Unknown', 'Nothing read yet'],
    ['fr', 'Inconnu', "Rien n'a encore été relevé"],
    ['sv', 'Okänt', 'Inget har lästs ännu'],
  ])('shows what was never read as unknown, in %s', async (locale, unknownText, never) => {
    i18n.global.locale.value = locale
    const w = await mounted(unknown)
    const values = w.findAll('.reading .value').map((v) => v.text())
    expect(values).toHaveLength(10)
    expect(new Set(values)).toEqual(new Set([unknownText]))
    expect(w.find('.charging-status').text()).toContain(unknownText)
    expect(w.text()).toContain(never)
    expect(w.findAll('time')).toHaveLength(0)
    expect(w.find('a[target="_blank"]').exists()).toBe(false)
  })

  it.each<[Locale, string, string]>([
    ['en', 'Battery capacity', 'Usable, from the variant'],
    ['fr', 'Capacité de la batterie', 'Utile, selon la variante'],
    ['sv', 'Batterikapacitet', 'Användbar, enligt varianten'],
  ])(
    'falls back on the variant where the car reports no capacity, in %s',
    async (locale, label, note) => {
      i18n.global.locale.value = locale
      fetchVehicle.mockResolvedValue(vehicle)
      const w = await mounted({ ...state, battery_capacity_kwh: null })
      expect(reading(w, label)).toEqual({ value: '64 kWh', details: note, stale: false })
    },
  )

  it('keeps the capacity the car reports over the variant', async () => {
    fetchVehicle.mockResolvedValue(vehicle)
    const w = await mounted(state)
    expect(reading(w, 'Battery capacity').value).toBe('78 kWh')
    expect(w.find('.variant-capacity').exists()).toBe(false)
  })

  // The way to connect again is the shell's, on every page of the vehicle.
  it('marks every value stale once the connection is lost', async () => {
    const lost = { ...state, connection: unknown.connection }
    const w = await mounted(lost)
    expect(w.findAll('.reading').every((r) => r.classes().includes('stale'))).toBe(true)
    expect(w.find('.v-alert').exists()).toBe(false)
  })

  it('keeps the values when a refresh fails, and says so', async () => {
    fetchVehicleState.mockResolvedValue(state)
    const { wrapper: w, queryClient } = mountWith(VehicleState, {
      props: { vehicle: state.vehicle_id },
    })
    await flushPromises()
    fetchVehicleState.mockRejectedValue(new ApiError(502, 'unavailable', ''))
    await queryClient.refetchQueries()
    await flushPromises()
    expect(reading(w, 'State of charge').value).toBe('64%')
    expect(w.find('.v-alert').text()).toContain('The state could not be refreshed')
  })

  it.each([
    [new ApiError(404, 'not_found', ''), 'This vehicle does not exist.'],
    [new ApiError(502, 'unavailable', ''), 'The state could not be loaded.'],
  ])('tells why there is no state: %s', async (e, message) => {
    fetchVehicleState.mockRejectedValue(e)
    const { wrapper } = mountWith(VehicleState, { props: { vehicle: 'nope' } })
    await flushPromises()
    expect(wrapper.find('.v-alert').text()).toContain(message)
  })

  // A <dl> holds only its dt/dd groups.
  it('titles its cards with headings, and keeps its lists of values valid', async () => {
    const w = await mounted(state)
    expect(w.findAll('.v-card-title').map((h) => h.element.tagName)).toEqual([
      'H2',
      'H2',
      'H2',
      'H2',
    ])
    for (const dl of w.findAll('dl')) {
      for (const child of dl.element.children) {
        expect(child.tagName).toBe('DIV')
        expect([...child.children].map((c) => c.tagName)).toEqual(['DT', 'DD'])
      }
    }
    expect(w.find('[role="meter"]').attributes('aria-valuenow')).toBe('64')
  })

  it('says a car asleep or out of reach is left alone, since when, nothing when awake', async () => {
    const at = {
      reported_at: null,
      fetched_at: '2026-09-28T18:40:00Z',
      checked_at: '2026-09-28T19:06:00Z',
    }
    let w = await mounted({ ...state, sleep: { ...at, value: 'awake' } })
    expect(w.find('.sleep').exists()).toBe(false)
    w.unmount()
    w = await mounted({ ...state, sleep: { ...at, value: 'asleep' } })
    expect(w.findAll('.sleep > *').map((e) => plain(e.text()))).toEqual([
      'Asleep',
      'since 18:40',
      'Runsten lets it sleep: what it measures is read when it wakes up.',
    ])
    expect(w.find('.sleep time').attributes('datetime')).toBe(at.fetched_at)
    w.unmount()
    w = await mounted({ ...state, sleep: { ...at, value: 'offline' } })
    // Out of reach may be awake without a network: not said to sleep.
    expect(w.findAll('.sleep > *').map((e) => plain(e.text()))).toEqual([
      'Out of reach',
      'since 18:40',
      'Runsten reads it again as soon as Tesla can reach it.',
    ])
    // A car that tells nothing of its sleep (a Volvo): nothing either.
    w.unmount()
    w = await mounted(state)
    expect(w.find('.sleep').exists()).toBe(false)
  })

  it('tells the software and the update waiting for it, nothing for a car without', async () => {
    const at = {
      reported_at: null,
      fetched_at: '2026-09-28T18:40:00Z',
      checked_at: '2026-09-28T19:06:00Z',
    }
    const update = {
      status: 'downloading' as const,
      version: '2026.38.3.2',
      download_pct: 40,
      install_pct: null,
      duration_s: 2700,
    }
    let w = await mounted({
      ...state,
      software: { ...at, value: '2026.38.3.1' },
      software_update: { ...at, value: update },
    })
    expect(reading(w, 'Software').value).toBe('2026.38.3.1')
    expect(reading(w, 'Software update').value).toBe('2026.38.3.2: Downloading, 40%')
    // Waiting for Wi-Fi: no progress to tell; without its version, the status alone.
    w.unmount()
    w = await mounted({
      ...state,
      software: { ...at, value: '2026.38.3.1' },
      software_update: { ...at, value: { ...update, status: 'waiting_wifi', version: null } },
    })
    expect(reading(w, 'Software update').value).toBe('Waiting for Wi-Fi')
    w.unmount()
    w = await mounted(state)
    expect(() => reading(w, 'Software')).toThrow()
    expect(() => reading(w, 'Software update')).toThrow()
  })

  it("tells the car's latest measure once it is older than half an hour", async () => {
    let w = await mounted(state)
    expect(w.find('.reported').exists()).toBe(false)
    w.unmount()
    vi.setSystemTime(Date.parse('2026-09-28T19:32:01Z'))
    w = await mounted(state)
    // The latest of the car's own times: the battery's, though the odometer's is older.
    expect(w.find('.reported').text()).toBe('The car last measured 19:02')
    expect(w.find('.reported').attributes('datetime')).toBe('2026-09-28T19:02:00Z')
  })
})
