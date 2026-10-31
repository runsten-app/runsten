import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { h } from 'vue'
import { ApiError } from '@/shared/api'
import { i18n, type Locale } from '@/shared/i18n'
import { VehicleState } from '@/widgets/vehicle-state'
import charging from '@fixtures/charging.json'
import state from '@fixtures/state.json'
import unknown from '@fixtures/state-unknown.json'
import vehicle from '@fixtures/vehicle-model.json'
import { mountWith } from '@test/utils'

const { fetchVehicle, fetchVehicleState, fetchChargeInProgress } = vi.hoisted(() => ({
  fetchVehicle: vi.fn(),
  fetchVehicleState: vi.fn(),
  fetchChargeInProgress: vi.fn(),
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchChargeInProgress,
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
  fetchChargeInProgress.mockReset()
  fetchChargeInProgress.mockResolvedValue(null)
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
  it('leads to the charge in progress, live', async () => {
    fetchChargeInProgress.mockResolvedValue(charging.charge)
    const w = await mounted(state)
    expect(fetchChargeInProgress).toHaveBeenCalledWith(state.vehicle_id)
    expect(w.find('a.charge-link').attributes('href')).toBe(
      `/vehicles/${state.vehicle_id}/charges/${charging.charge?.id}`,
    )
  })

  // Unplugged, there can be none: nothing is asked.
  it('asks for no charge in progress of a vehicle unplugged', async () => {
    const w = await mounted(unknown)
    expect(fetchChargeInProgress).not.toHaveBeenCalled()
    expect(w.find('a.charge-link').exists()).toBe(false)
  })

  it('shows each value with its freshness', async () => {
    const w = await mounted(state)
    expect(plain(w.text())).toContain('Last checked 3 min ago')
    // Said once for the tile, repeated by a value only where it differs.
    expect(provenance(w, 'Battery')).toBe('checked 3 min ago · since 19:03')
    expect(reading(w, 'State of charge')).toEqual({ value: '64%', details: '', stale: false })
    expect(reading(w, 'Range').value).toBe('256 km')
    expect(reading(w, 'Odometer')).toMatchObject({ value: '12,473 km', stale: false })
    // The mode's pill says the engine: no value repeats it.
    expect(() => reading(w, 'Engine')).toThrow()
    // Read once a day: not stale after 14 hours.
    expect(reading(w, 'Battery capacity')).toMatchObject({ value: '78 kWh', stale: false })
    expect(reading(w, 'Battery capacity').details).toMatch(/^checked .+ · since /)
    // Without a map, the coordinates are a value of the vehicle's tile, with their own times.
    expect(reading(w, 'Last position').value).toBe('45.76400, 4.83570')
    expect(reading(w, 'Last position').details).toBe('checked 36 min ago')
    expect(provenance(w, 'Vehicle')).toMatch(/^checked .+ · since /)
  })

  it('puts a charge in progress forward, in a tile of its own', async () => {
    const w = await mounted(state)
    expect(plain(w.find('.mode').text())).toBe('Charging')
    expect(w.find('.charge-line').exists()).toBe(false)
    const tile = w.find('.v-card[data-test="charging"]')
    expect(tile.find('h2').text()).toContain('Charging')
    // The status is the tile's pill, named for a screen reader.
    const status = tile.find('.charging-status')
    expect(plain(status.text())).toBe('Status: Charging')
    expect(status.classes()).toContain('status-pill--success')
    expect(reading(w, 'Power').value).toBe('7.4 kW')
    expect(reading(w, 'Type').value).toBe('AC')
    expect(reading(w, 'Cable').value).toBe('Connected')
    // The target is the bar's, said once.
    expect(() => reading(w, 'Target')).toThrow()
    expect(plain(w.find('.under-bar .target').text())).toBe('Target 80%')
  })

  it.each(['done', 'idle', 'scheduled'])(
    'says a charge %s in one line under the battery',
    async (value) => {
      const parked = {
        ...state,
        charging: { ...state.charging, status: { ...state.charging.status, value } },
      }
      const w = await mounted(parked)
      expect(w.find('.v-card[data-test="charging"]').exists()).toBe(false)
      const line = w.find('.battery .charge-line')
      expect(line.find('h3').text()).toBe('Charging')
      expect(line.attributes('aria-labelledby')).toBe(line.find('h3').attributes('id'))
      expect(plain(line.find('.charging-status').text())).toMatch(/^Status: /)
      expect(reading(w, 'Cable').value).toBe('Connected')
      expect(reading(w, 'Type').value).toBe('AC')
      // No power to tell while nothing charges, the target under the bar only.
      expect(() => reading(w, 'Power')).toThrow()
      expect(() => reading(w, 'Target')).toThrow()
    },
  )

  it('keeps the target in the line where no state of charge draws a bar', async () => {
    const parked = {
      ...state,
      soc_pct: null,
      charging: { ...state.charging, status: { ...state.charging.status, value: 'done' } },
    }
    const w = await mounted(parked)
    expect(reading(w, 'Target').value).toBe('80%')
    // Its times, which the bar's line no longer says.
    expect(w.find('.charge-line .details').exists()).toBe(true)
  })

  it('takes a power above zero without a status for a charge, not a zero one', async () => {
    const power = (value: number) => ({
      ...state,
      charging: {
        ...state.charging,
        status: null,
        power_w: { ...state.charging.cable, value },
      },
    })
    let w = await mounted(power(7400))
    expect(w.find('.v-card[data-test="charging"]').exists()).toBe(true)
    expect(reading(w, 'Power').value).toBe('7.4 kW')
    w.unmount()
    w = await mounted(power(0))
    expect(w.find('.charge-line').exists()).toBe(true)
    expect(() => reading(w, 'Power')).toThrow()
  })

  it('dates the line by the values it shows, not by a power it does not', async () => {
    const at = (t: string) => ({ reported_at: null, fetched_at: t, checked_at: t })
    const w = await mounted({
      ...state,
      charging: {
        ...state.charging,
        status: { ...at('2026-09-28T18:00:00Z'), value: 'done' },
        power_w: { ...at('2026-09-28T12:00:00Z'), value: 0 },
      },
    })
    const times = w.findAll('.charge-line .details time').map((t) => t.attributes('datetime'))
    expect(times).toEqual(['2026-09-28T18:00:00Z', '2026-09-28T18:00:00Z'])
  })

  it.each(['discharging', 'error'])('gives a charge %s its tile', async (value) => {
    const w = await mounted({
      ...state,
      charging: { ...state.charging, status: { ...state.charging.status, value } },
    })
    expect(w.find('.v-card[data-test="charging"]').exists()).toBe(true)
    expect(w.find('.charge-line').exists()).toBe(false)
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

  it('tells an unknown power during a charge', async () => {
    const charging = { ...state, charging: { ...state.charging, power_w: null } }
    expect(reading(await mounted(charging), 'Power').value).toBe('Unknown')
    i18n.global.locale.value = 'fr'
    expect(reading(await mounted(charging), 'Puissance').value).toBe('Inconnu')
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
    // The battery's three, the odometer, the position, and the charge's line: its cable, type and
    // target (no bar to show it).
    expect(values).toHaveLength(8)
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

  it('keeps its aside when the state cannot be loaded', async () => {
    fetchVehicleState.mockRejectedValue(new ApiError(502, 'unavailable', ''))
    const host = {
      render: () =>
        h(
          VehicleState,
          { vehicle: 'v1' },
          { aside: () => h('p', { class: 'aside' }, 'Last days') },
        ),
    }
    const { wrapper } = mountWith(host)
    await flushPromises()
    expect(wrapper.find('.v-alert').text()).toContain('The state could not be loaded.')
    expect(wrapper.find('.aside').text()).toBe('Last days')
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
    expect(w.findAll('.v-card-title').map((h) => h.element.tagName)).toEqual(['H2', 'H2', 'H2'])
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
      scheduled_at: null,
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
    // Scheduled: when, as the owner set it.
    w.unmount()
    w = await mounted({
      ...state,
      software: { ...at, value: '2026.38.3.1' },
      software_update: {
        ...at,
        value: { ...update, status: 'scheduled', scheduled_at: '2026-09-29T01:50:00Z' },
      },
    })
    expect(reading(w, 'Software update').value).toBe(
      '2026.38.3.2: Scheduled for 29 Sept 2026, 01:50',
    )
    w.unmount()
    w = await mounted(state)
    expect(() => reading(w, 'Software')).toThrow()
    expect(() => reading(w, 'Software update')).toThrow()
  })

  it('says in a line whether the car is locked and what is open, nothing for a car without', async () => {
    const at = {
      reported_at: '2026-09-28T19:00:00Z',
      fetched_at: '2026-09-28T19:00:30Z',
      checked_at: '2026-09-28T19:05:00Z',
    }
    const closed = {
      front_left: 'closed',
      front_right: 'closed',
      rear_left: 'closed',
      rear_right: 'closed',
    } as const
    const doors = {
      ...closed,
      locked: true,
      hood: 'closed',
      tailgate: 'closed',
      tank_lid: null,
      sentry: null,
    } as const
    const windows = { ...closed, sunroof: null } as const
    // Each part of the line: the heading, the pill and the facts.
    const line = (w: Awaited<ReturnType<typeof mounted>>) =>
      w
        .findAll('[data-test="openings"] .row > *')
        .map((e) => plain(e.text()))
        .join(' | ')
    let w = await mounted({
      ...state,
      doors: { ...at, value: doors },
      windows: { ...at, value: windows },
    })
    expect(line(w)).toBe('Doors and windows | Locked | All closed')
    expect(w.find('[data-test="openings"] h3').text()).toBe('Doors and windows')
    expect(plain(w.find('[data-test="openings"] .details').text())).toBe(
      'checked 1 min ago · since 19:00',
    )
    expect(w.find('[data-test="openings"] .measured').exists()).toBe(false)
    w.unmount()
    // Unlocked, watching, a window open and a door ajar; measured hours before: said.
    w = await mounted({
      ...state,
      doors: {
        ...at,
        reported_at: '2026-09-28T15:00:00Z',
        value: { ...doors, locked: false, rear_left: 'ajar', sentry: true },
      },
      windows: { ...at, value: { ...windows, front_left: 'open', rear_right: 'open' } },
    })
    expect(line(w)).toBe(
      'Doors and windows | Unlocked | Open: front left window and rear right window | Ajar: rear left door | Sentry Mode on',
    )
    expect(w.find('[data-test="openings"] .measured').text()).toBe('The car last measured 15:00')
    w.unmount()
    // The windows alone, the lock untold: no pill, and closed said of the windows only.
    w = await mounted({ ...state, windows: { ...at, value: windows } })
    expect(line(w)).toBe('Doors and windows | Windows closed')
    w.unmount()
    // The doors alone: closed said of the doors only.
    w = await mounted({ ...state, doors: { ...at, value: doors } })
    expect(line(w)).toBe('Doors and windows | Locked | Doors closed')
    w.unmount()
    // Read apart and the windows checked longer ago: the line takes their times, and the
    // older of the car's measures.
    w = await mounted({
      ...state,
      doors: { ...at, value: doors },
      windows: {
        reported_at: '2026-09-28T14:00:00Z',
        fetched_at: '2026-09-28T16:00:00Z',
        checked_at: '2026-09-28T18:00:00Z',
        value: windows,
      },
    })
    expect(plain(w.find('[data-test="openings"] .details').text())).toBe(
      'checked 1 hr ago · since 16:00',
    )
    expect(w.find('[data-test="openings"] .measured').text()).toBe('The car last measured 14:00')
    w.unmount()
    w = await mounted(state)
    expect(w.find('[data-test="openings"]').exists()).toBe(false)
  })

  it('tells the temperatures and the climate that runs, nothing for a car without', async () => {
    const at = {
      reported_at: null,
      fetched_at: '2026-09-28T18:40:00Z',
      checked_at: '2026-09-28T19:06:00Z',
    }
    const climate = {
      outside_temp_c: { ...at, value: -3.5 },
      cabin_temp_c: { ...at, value: 19.5 },
      on: { ...at, value: true },
      preconditioning: { ...at, value: false },
      battery_heater: { ...at, value: true },
    }
    let w = await mounted({ ...state, climate })
    expect(reading(w, 'Outside').value).toBe('-3.5°C')
    expect(reading(w, 'Cabin').value).toBe('19.5°C')
    expect(w.find('.climate-running').text()).toBe('Climate on')
    expect(w.find('.heater').text()).toBe('Battery heating')
    // Preconditioning says it, which runs the climate too; the heater off says nothing.
    w.unmount()
    w = await mounted({
      ...state,
      climate: {
        ...climate,
        preconditioning: { ...at, value: true },
        battery_heater: { ...at, value: false },
      },
    })
    expect(w.find('.climate-running').text()).toBe('Preconditioning')
    expect(w.find('.heater').exists()).toBe(false)
    // Without the cabin's temperature, nor a state of charge: what runs still says itself.
    w.unmount()
    w = await mounted({
      ...state,
      soc_pct: null,
      climate: { ...climate, cabin_temp_c: null },
    })
    expect(() => reading(w, 'Cabin')).toThrow()
    expect(reading(w, 'Climate').value).toBe('Climate on')
    expect(w.find('.heater').text()).toBe('Battery heating')
    // A car without: neither the temperatures nor an Unknown in their place.
    w.unmount()
    w = await mounted(state)
    expect(() => reading(w, 'Outside')).toThrow()
    expect(() => reading(w, 'Cabin')).toThrow()
    expect(w.find('.climate-running').exists()).toBe(false)
    expect(() => reading(w, 'Climate')).toThrow()
    expect(w.find('.heater').exists()).toBe(false)
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
