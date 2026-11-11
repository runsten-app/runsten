import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Stats } from '@/entities/stats'
import type { Vehicle } from '@/entities/vehicle'
import GaragePage from '@/pages/home/ui/GaragePage.vue'
import battery from '@fixtures/battery.json'
import state from '@fixtures/state.json'
import stats from '@fixtures/stats.json'
import vehicles from '@fixtures/vehicles.json'
import { sessionKeys } from '@/entities/session'
import { heard, mountWith, seen, testQueryClient, testRouter } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const { fetchStats, fetchVehicleState, getBattery, fetchSession } = vi.hoisted(() => ({
  fetchStats: vi.fn(),
  fetchVehicleState: vi.fn(),
  getBattery: vi.fn(),
  fetchSession: vi.fn(),
}))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicleState,
}))
vi.mock('@/entities/battery', async (original) => ({
  ...(await original<typeof import('@/entities/battery')>()),
  getBattery,
}))
vi.mock('@/entities/session', async (original) => ({
  ...(await original<typeof import('@/entities/session')>()),
  fetchSession,
}))

enableAutoUnmount(afterEach)
afterEach(() => {
  vi.useRealTimers()
})

type Item = (typeof vehicles.items)[0]
const [xc40, , ex30] = vehicles.items as [Item, Item, Item]
const two = [xc40, ex30] as unknown as Vehicle[]

// The first vehicle's trips all known, so that its consumption is ranked.
const first: Stats = {
  ...(stats as Stats),
  totals: {
    ...(stats as Stats).totals,
    trips: { ...(stats as Stats).totals.trips, energy_unknown: 0 },
  },
}

// The second vehicle drives at 25 kWh/100 km for 300 km, charged only on DC, and tells no
// outside temperature: the first is the better one, and the only one on the temperature's
// chart.
const other: Stats = {
  ...(stats as Stats),
  totals: {
    ...(stats as Stats).totals,
    trips: {
      ...(stats as Stats).totals.trips,
      distance_km: 300,
      energy_unknown: 0,
      consumption_kwh_per_100km: 25,
    },
    charges: {
      ...(stats as Stats).totals.charges,
      cost: { min_minor: 9000, max_minor: 9000 },
    },
  },
  trips_by_temperature: { bands: [], left_out: 3 },
  charges_by_place: {
    places: [],
    outside: {
      ...(stats as Stats).charges_by_place.outside,
      dc: { ...(stats as Stats).charges_by_place.outside.dc, count: 1, energy_soc_kwh: 40 },
    },
    no_position: { ...(stats as Stats).charges_by_place.no_position, energy_soc_kwh: 0 },
  },
}

beforeEach(() => {
  // A few minutes after the state's readings: current.
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T19:10:00Z'))
  fetchStats.mockReset()
  fetchVehicleState.mockReset()
  getBattery.mockReset()
  fetchSession.mockReset()
  fetchStats.mockImplementation((id: string) => Promise.resolve(id === xc40.id ? first : other))
  fetchVehicleState.mockImplementation((id: string) =>
    Promise.resolve(
      id === xc40.id
        ? state
        : {
            ...state,
            charging: { ...state.charging, status: null },
            engine: { ...state.engine, value: 'running' },
          },
    ),
  )
  getBattery.mockResolvedValue(battery)
})

// garage mounts the page on a path, the session in the cache as the router's guard leaves it.
async function garage(path = '/', limits: unknown = null) {
  const router = testRouter([
    { path: '/vehicles/:vehicle', name: 'vehicle', component: { render: () => null } },
  ])
  await router.push(path)
  const queryClient = testQueryClient()
  queryClient.setQueryData(sessionKeys.current(), { limits })
  const { wrapper } = mountWith(GaragePage, { router, queryClient, props: { vehicles: two } })
  await flushPromises()
  return { wrapper, router }
}

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

describe('GaragePage', () => {
  it('adds the vehicles up over the last 30 days, and says what they do now', async () => {
    const { wrapper } = await garage()
    const lead = plain(wrapper.find('.garage-totals .lead').text())
    expect(lead).toBe('2 vehicles · last 30 days · 1 driving and 1 charging now')
    expect(plain(wrapper.find('.garage-totals .distance dd').text())).toBe('373 km')
    // €13.74–€14.29 and €90.00.
    const cost = wrapper.find('.garage-totals .cost dd').element
    expect(seen(cost)).toBe('€103.74 – €104.29')
    expect(heard(cost)).toBe('from €103.74 to €104.29')
    // Over 373 km.
    expect(seen(wrapper.find('.garage-totals .per-hundred dd').element)).toBe('€27.81 – €27.96')
    // Both read over the last 30 days.
    const asked = fetchStats.mock.calls.map((c) => c[1] as { bucket?: string })
    expect(asked.some((q) => !q.bucket)).toBe(true)
    expect(asked.some((q) => q.bucket === 'month')).toBe(true)
  })

  it('shows a tile per vehicle, leading to its page, the best figures marked', async () => {
    const { wrapper } = await garage()
    const tiles = wrapper.findAll('.vehicle-tile')
    expect(tiles.map((t) => t.find('h2 a').text())).toEqual([
      'XC40 Recharge Twin · 2021',
      'EX30 Single Motor Extended Range · 2024',
    ])
    expect(tiles[0]?.find('h2 a').attributes('href')).toBe(`/vehicles/${xc40.id}`)
    expect(plain(tiles[0]?.find('.doing').text())).toBe('Charging to 80%')
    expect(plain(tiles[1]?.find('.doing').text())).toBe('Driving')
    expect(plain(tiles[0]?.find('.consumption .figure').text())).toBe('18.8 kWh/100 km')
    expect(tiles[0]?.find('.consumption .best').text()).toBe('Lowest')
    expect(plain(tiles[1]?.find('.consumption .figure').text())).toBe('25 kWh/100 km')
    expect(tiles[1]?.find('.consumption .best').exists()).toBe(false)
    expect(tiles[0]?.find('.cost .best').exists()).toBe(true)
    expect(tiles[1]?.find('.cost .best').exists()).toBe(false)
  })

  it('ranks no consumption estimated over part of the trips, nor a distance partly known', async () => {
    fetchStats.mockImplementation((id: string) => Promise.resolve(id === xc40.id ? stats : other))
    const { wrapper } = await garage()
    const tiles = wrapper.findAll('.vehicle-tile')
    // Its energy unknown for two trips: shown, never ranked.
    expect(plain(tiles[0]?.find('.consumption .figure').text())).toBe('18.8 kWh/100 km')
    expect(wrapper.find('.consumption .best').exists()).toBe(false)
  })

  it('says when a state no longer current was read, and counts it in nothing now', async () => {
    vi.setSystemTime(new Date('2026-09-29T19:10:00Z'))
    const { wrapper } = await garage()
    const tiles = wrapper.findAll('.vehicle-tile')
    expect(plain(tiles[0]?.find('.doing').text())).toBe('Last read yesterday')
    expect(plain(wrapper.find('.garage-totals .lead').text())).toBe('2 vehicles · last 30 days')
  })

  it('hides a vehicle from the comparison, its tile kept', async () => {
    const { wrapper, router } = await garage('/?compare=charging')
    const toggle = wrapper.findAll('.vehicle-tile .toggle')[0]
    expect(toggle?.attributes('aria-pressed')).toBe('false')
    await toggle?.trigger('click')
    await flushPromises()
    expect(toggle?.attributes('aria-pressed')).toBe('true')
    expect(wrapper.findAll('.vehicle-tile')).toHaveLength(2)
    // Nothing marked best with one vehicle compared.
    expect(wrapper.find('.vehicle-tile .best').exists()).toBe(false)
    const bar = wrapper.find('.charge-split').findComponent({ name: 'Bar' })
    const data = bar.props('data') as { labels: string[] }
    expect(data.labels).toEqual(['EX30 Single Motor Extended Range · 2024'])
    expect(router.currentRoute.value.query.compare).toBe('charging')
  })

  it('draws the consumption by temperature of the vehicles that tell it', async () => {
    const { wrapper } = await garage()
    const line = wrapper.find('.by-temperature').findComponent({ name: 'Line' })
    const data = line.props('data') as {
      labels: string[]
      datasets: { label: string; data: (number | null)[] }[]
    }
    expect(data.labels).toEqual(['5 to 10°C'])
    expect(data.datasets.map((d) => [d.label, d.data])).toEqual([
      ['XC40 Recharge Twin · 2021', [18.75]],
    ])
    expect(wrapper.find('.by-temperature .hint').text()).toBe(
      "EX30 Single Motor Extended Range · 2024 has no consumption by outside temperature (the car tells none, or no trip's energy is known), and is not drawn.",
    )
  })

  it('draws no vehicle whose bands have no estimate', async () => {
    const band = (stats as Stats).trips_by_temperature.bands[0]
    const hybrid: Stats = {
      ...other,
      trips_by_temperature: {
        bands: band ? [{ ...band, consumption_kwh_per_100km: null }] : [],
        left_out: 0,
      },
    }
    fetchStats.mockImplementation((id: string) => Promise.resolve(id === xc40.id ? first : hybrid))
    const { wrapper } = await garage()
    const line = wrapper.find('.by-temperature').findComponent({ name: 'Line' })
    const data = line.props('data') as { datasets: { label: string }[] }
    expect(data.datasets.map((d) => d.label)).toEqual(['XC40 Recharge Twin · 2021'])
  })

  it('splits the charging by where, and reads the battery only for its view', async () => {
    const { wrapper } = await garage('/?compare=charging')
    expect(getBattery).not.toHaveBeenCalled()
    const bar = wrapper.find('.charge-split').findComponent({ name: 'Bar' })
    const data = bar.props('data') as {
      datasets: { label: string; data: (number | null)[] }[]
    }
    const by = Object.fromEntries(data.datasets.map((d) => [d.label, d.data]))
    expect(by['At your places']?.[0]).toBeCloseTo(60)
    expect(by['DC']).toEqual([0, 100])
    // The fixture's charges are all of known energy, and both vehicles charged.
    expect(wrapper.find('.charge-split .hint').text()).toBe(
      'Your places are those of the settings, with their tariffs: they explain most differences in cost.',
    )
    await wrapper.find('.compare-toggle').findAll('button')[3]?.trigger('click')
    await flushPromises()
    expect(getBattery).toHaveBeenCalledTimes(2)
    const capacity = wrapper.find('.capacity').findComponent({ name: 'Bar' })
    const c = capacity.props('data') as { datasets: { data: (number | null)[] }[] }
    expect(c.datasets.map((d) => d.data)).toEqual([
      [-2, null],
      [null, -2],
    ])
  })

  it('compares what 100 km cost each vehicle by month', async () => {
    const { wrapper } = await garage('/?compare=cost&period=12m')
    const bar = wrapper.find('.cost-by-month').findComponent({ name: 'Bar' })
    const data = bar.props('data') as { datasets: { label: string; data: (number | null)[] }[] }
    expect(data.datasets.map((d) => d.label)).toEqual([
      'XC40 Recharge Twin · 2021',
      'EX30 Single Motor Extended Range · 2024',
    ])
    // The totals of the period unsplit, every account has them; the months split.
    const asked = fetchStats.mock.calls.map((c) => c[1] as { bucket?: string })
    expect(asked.filter((q) => q.bucket === 'month')).toHaveLength(2)
    expect(asked.filter((q) => !q.bucket)).toHaveLength(2)
    expect(wrapper.find('.garage-compare .scope').text()).toBe(
      '2 vehicles over the same period: last 12 months',
    )
  })

  it('compares the loss while parked, where it is known', async () => {
    const { wrapper } = await garage('/?compare=battery')
    // A bar per vehicle, in its color, at its own place: a loss of 0 is one.
    const bar = wrapper.find('.parked').findComponent({ name: 'Bar' })
    const data = bar.props('data') as { labels: string[]; datasets: { data: (number | null)[] }[] }
    expect(data.labels).toHaveLength(2)
    expect(data.datasets.map((d) => d.data)).toEqual([
      [0, null],
      [null, 0],
    ])
  })

  it('stacks the spending of every vehicle by month', async () => {
    const { wrapper } = await garage()
    const bar = wrapper.find('.garage-spending').findComponent({ name: 'Bar' })
    const data = bar.props('data') as { datasets: { label: string }[] }
    expect(data.datasets.map((d) => d.label)).toEqual([
      'XC40 Recharge Twin · 2021',
      'EX30 Single Motor Extended Range · 2024',
    ])
  })

  it("keeps the period's totals where the account's limits refuse the split", async () => {
    const limits = { history_from: null, unavailable: ['stats'], unread_vehicles: [] }
    const { wrapper } = await garage('/?compare=cost', limits)
    // Never a split: the API refuses it.
    expect(fetchStats.mock.calls.every((c) => !(c[1] as { bucket?: string }).bucket)).toBe(true)
    expect(wrapper.findAll('.vehicle-tile .figures')).toHaveLength(2)
    expect(plain(wrapper.find('.garage-totals .distance dd').text())).toBe('373 km')
    expect(wrapper.find('.garage-compare .feature-unavailable').exists()).toBe(true)
    expect(wrapper.find('.garage-spending .feature-unavailable').exists()).toBe(true)
  })

  it('waits for the months before saying a currency is missing', async () => {
    fetchStats.mockImplementation((id: string, q: { bucket?: string }) =>
      q.bucket ? new Promise(() => {}) : Promise.resolve(id === xc40.id ? first : other),
    )
    const { wrapper } = await garage('/?compare=cost')
    expect(wrapper.find('.garage-compare .v-progress-linear').exists()).toBe(true)
    expect(wrapper.find('.garage-compare .v-alert').exists()).toBe(false)
  })

  it('says the spending leaves out the costs unknown', async () => {
    const partly: Stats = {
      ...other,
      totals: { ...other.totals, charges: { ...other.totals.charges, cost_unknown: 1 } },
    }
    fetchStats.mockImplementation((id: string) => Promise.resolve(id === xc40.id ? first : partly))
    const { wrapper } = await garage()
    expect(wrapper.find('.garage-spending .hint').text()).toContain(
      'The cost of 1 charge is unknown and left out.',
    )
  })

  it('says why when no vehicle can be drawn by temperature or by charging', async () => {
    const band = (stats as Stats).trips_by_temperature.bands[0]
    const blind = (s: Stats): Stats => ({
      ...s,
      trips_by_temperature: {
        bands: band ? [{ ...band, consumption_kwh_per_100km: null }] : [],
        left_out: 0,
      },
      totals: { ...s.totals, charges: { ...s.totals.charges, energy_soc_unknown: 2 } },
      charges_by_place: {
        places: [],
        outside: {
          ac: { ...s.charges_by_place.outside.ac, energy_soc_kwh: 0 },
          dc: { ...s.charges_by_place.outside.dc, energy_soc_kwh: 0 },
          unknown: { ...s.charges_by_place.outside.unknown, energy_soc_kwh: 0 },
        },
        no_position: { ...s.charges_by_place.no_position, energy_soc_kwh: 0 },
      },
    })
    fetchStats.mockImplementation((id: string) =>
      Promise.resolve(blind(id === xc40.id ? first : other)),
    )
    const { wrapper, router } = await garage()
    expect(wrapper.find('.by-temperature .empty').text()).toBe(
      "XC40 Recharge Twin · 2021 and EX30 Single Motor Extended Range · 2024 have no consumption by outside temperature (the cars tell none, or no trip's energy is known), and are not drawn.",
    )
    await router.replace('/?compare=charging')
    await flushPromises()
    expect(wrapper.find('.charge-split .empty').text()).toBe(
      'XC40 Recharge Twin · 2021 and EX30 Single Motor Extended Range · 2024 have charges whose energy is unknown, left out of their shares.',
    )
  })

  it('tells a vehicle without a charge from one whose charges have no known energy', async () => {
    const idle: Stats = {
      ...other,
      totals: { ...other.totals, charges: { ...other.totals.charges, count: 0 } },
      charges_by_place: {
        ...other.charges_by_place,
        outside: {
          ...other.charges_by_place.outside,
          dc: { ...other.charges_by_place.outside.dc, energy_soc_kwh: 0 },
        },
      },
    }
    const blind: Stats = {
      ...first,
      totals: { ...first.totals, charges: { ...first.totals.charges, energy_soc_unknown: 2 } },
    }
    fetchStats.mockImplementation((id: string) => Promise.resolve(id === xc40.id ? blind : idle))
    const { wrapper } = await garage('/?compare=charging')
    expect(wrapper.find('.charge-split .hint').text()).toBe(
      'EX30 Single Motor Extended Range · 2024 did not charge over the period. XC40 Recharge Twin · 2021 has charges whose energy is unknown, left out of its shares.',
    )
  })
})
