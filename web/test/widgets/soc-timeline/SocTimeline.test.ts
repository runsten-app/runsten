import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Band, Series } from '@/shared/ui/chart'
import { SocTimeline } from '@/widgets/soc-timeline'
import charges from '@fixtures/charges.json'
import series from '@fixtures/series.json'
import versions from '@fixtures/software-versions.json'
import trips1 from '@fixtures/trips-page-1.json'
import trips2 from '@fixtures/trips-page-2.json'
import { mountWith } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const mocks = vi.hoisted(() => ({
  fetchSeries: vi.fn(),
  fetchTrips: vi.fn(),
  fetchCharges: vi.fn(),
  fetchSoftwareVersions: vi.fn(),
}))
vi.mock('@/entities/series', async (original) => ({
  ...(await original<typeof import('@/entities/series')>()),
  fetchSeries: mocks.fetchSeries,
}))
vi.mock('@/entities/trip', async (original) => ({
  ...(await original<typeof import('@/entities/trip')>()),
  fetchTrips: mocks.fetchTrips,
}))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  fetchSoftwareVersions: mocks.fetchSoftwareVersions,
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchCharges: mocks.fetchCharges,
}))

const now = new Date('2026-09-28T19:02:00Z')
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(now)
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchSeries.mockResolvedValue(series)
  mocks.fetchTrips.mockImplementation((_v: string, q: { cursor?: string }) =>
    Promise.resolve(q.cursor ? trips2 : trips1),
  )
  mocks.fetchCharges.mockResolvedValue(charges)
  mocks.fetchSoftwareVersions.mockResolvedValue({ items: [], next_cursor: null })
})
afterEach(() => {
  vi.useRealTimers()
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mounted(week = false) {
  const { wrapper } = mountWith(SocTimeline, { props: { vehicle: 'v1' } })
  await flushPromises()
  if (week) {
    await wrapper
      .findAll('button')
      .find((b) => b.text() === '7 d')
      ?.trigger('click')
    await flushPromises()
  }
  return wrapper
}

describe('SocTimeline', () => {
  it('asks the last 24 hours, on a window that moves every five minutes', async () => {
    await mounted()
    expect(mocks.fetchSeries).toHaveBeenCalledWith('v1', {
      from: '2026-09-27T19:00:00.000Z',
      to: '2026-09-28T19:00:00.000Z',
    })
    expect(mocks.fetchTrips).toHaveBeenCalledWith('v1', {
      from: '2026-09-27T19:00:00.000Z',
      to: undefined,
      cursor: undefined,
    })
  })

  it('asks the last 7 days when the reader picks them', async () => {
    await mounted(true)
    expect(mocks.fetchSeries).toHaveBeenLastCalledWith('v1', {
      from: '2026-09-21T19:00:00.000Z',
      to: '2026-09-28T19:00:00.000Z',
    })
    expect(mocks.fetchTrips).toHaveBeenCalledWith('v1', {
      from: '2026-09-21T19:00:00.000Z',
      to: undefined,
      cursor: undefined,
    })
  })

  it('draws the state of charge over a strip of every trip and charge, parked between', async () => {
    const w = await mounted()
    const frame = w.findComponent({ name: 'ChartFrame' })
    const [soc] = frame.props('series') as Series[]
    expect(soc?.data).toHaveLength(6)
    expect(soc?.area).toBe(true)
    const bands = frame.props('bands') as Band[]
    // Every page of the trips: the second is loaded too.
    expect(mocks.fetchTrips).toHaveBeenCalledTimes(2)
    expect(bands.map((b) => b.label)).toEqual([
      'Parked',
      'Asleep',
      ...Array(trips1.items.length + trips2.items.length).fill('Driving'),
      'Charging',
      'Charging',
    ])
    // The evening charge: surely from 18:30 to 21:55, maybe from 18:20 to 21:56; the
    // reconstructed one is sure of nothing.
    const charging = bands.filter((b) => b.label === 'Charging')
    expect(charging[0]).toMatchObject({
      from: Date.parse('2026-09-28T18:20:00Z'),
      to: Date.parse('2026-09-28T21:56:00Z'),
      sure: { from: Date.parse('2026-09-28T18:30:00Z'), to: Date.parse('2026-09-28T21:55:00Z') },
    })
    expect(charging[1]?.sure).toBeUndefined()
    // Parked while read, up to the charge's earliest start; a lone reading spans nothing,
    // and nothing was read between the two runs.
    expect(bands[0]).toMatchObject({
      from: Date.parse('2026-09-28T17:26:00Z'),
      to: Date.parse('2026-09-28T18:20:00Z'),
    })
    expect(plain(w.find('canvas').attributes('aria-label'))).toBe(
      'Over the last 24 hours, from 50% to 50%, between 47% and 50%; 3 trips and 2 charges.',
    )
  })

  it("tells a trip's and a charge's figures under its times, each only when known", async () => {
    const w = await mounted()
    const bands = w.findComponent({ name: 'ChartFrame' }).props('bands') as Band[]
    const driving = bands.filter((b) => b.label === 'Driving')
    const charging = bands.filter((b) => b.label === 'Charging')
    expect(driving.map((b) => b.detail?.map(plain))).toEqual([
      [
        '28 Sept 2026',
        '16:35–16:41 – 17:25',
        '43–50 mins',
        'Elsewhere → Maison de Tante Agathe',
        '33 km',
      ],
      // A reconstructed trip: one interval, no duration, as in the list.
      ['28 Sept 2026', 'Happened between 12:00 and 13:00', '8 km'],
      [
        '28 Sept 2026',
        '06:55–07:01 – 07:40',
        '38–45 mins',
        'Maison de Tante Agathe → Elsewhere',
        '32 km · 62% → 55% · 18.8 kWh/100 km',
      ],
    ])
    expect(charging.map((b) => b.detail?.map(plain))).toEqual([
      [
        '28 Sept 2026',
        '18:20–18:30 – 21:56',
        '3 hrs, 25 mins – 3 hrs, 36 mins',
        'Maison de Tante Agathe',
        'AC · 47% → 80% · 26.4 kWh',
        '€5.24 – €5.79',
      ],
      ['28 Sept 2026', 'Happened between 01:00 and 04:00', '40% → 62% · 17.6 kWh'],
    ])
    // The table for screen readers lists the same.
    expect(w.findAll('.d-sr-only li').map((li) => plain(li.text()))).toContain(
      'Charging, 28 Sept 2026, 18:20–18:30 – 21:56, 3 hrs, 25 mins – 3 hrs, 36 mins, Maison de Tante Agathe, AC · 47% → 80% · 26.4 kWh, €5.24 – €5.79',
    )
  })

  it('tells asleep where the source told so, and parked where it was awake, outside the events', async () => {
    mocks.fetchSeries.mockResolvedValue({
      ...series,
      sleep: [
        // Offline across the morning trip (06:55–07:40): cut around it.
        { from: '2026-09-28T06:00:00Z', to: '2026-09-28T08:00:00Z', state: 'offline' },
        // Awake, with no energy read: parked all the same.
        { from: '2026-09-28T09:00:00Z', to: '2026-09-28T09:30:00Z', state: 'awake' },
        // Asleep over a run's readings: asleep, parked no more.
        { from: '2026-09-28T17:40:00Z', to: '2026-09-28T18:00:00Z', state: 'asleep' },
      ],
    })
    const w = await mounted()
    const bands = w.findComponent({ name: 'ChartFrame' }).props('bands') as Band[]
    const spans = (label: string) =>
      bands
        .filter((b) => b.label === label)
        .map((b) => [new Date(b.from).toISOString(), new Date(b.to).toISOString()])
    expect(spans('Asleep')).toEqual([
      ['2026-09-28T06:00:00.000Z', '2026-09-28T06:55:00.000Z'],
      ['2026-09-28T07:40:00.000Z', '2026-09-28T08:00:00.000Z'],
      ['2026-09-28T17:40:00.000Z', '2026-09-28T18:00:00.000Z'],
    ])
    expect(spans('Parked')).toEqual([
      ['2026-09-28T09:00:00.000Z', '2026-09-28T09:30:00.000Z'],
      ['2026-09-28T17:26:00.000Z', '2026-09-28T17:40:00.000Z'],
      ['2026-09-28T18:00:00.000Z', '2026-09-28T18:20:00.000Z'],
    ])
    // Offline is asleep's tint, the pointer telling which.
    const [offline] = bands.filter((b) => b.label === 'Asleep')
    expect(offline?.detail?.at(-1)).toBe('Offline: asleep or without a network')
    expect(bands.filter((b) => b.label === 'Asleep').at(-1)?.detail).toHaveLength(3)
  })

  it("tells an update's installation as far as a reading saw it, and none unseen", async () => {
    mocks.fetchSoftwareVersions.mockResolvedValue(versions)
    const w = await mounted()
    const bands = w.findComponent({ name: 'ChartFrame' }).props('bands') as Band[]
    // 2026.38.3.2 seen installing from 03:30–03:31, read from 05:00; the others never seen.
    const updates = bands.filter((b) => b.label === 'Update')
    expect(updates).toHaveLength(1)
    expect(updates[0]).toMatchObject({
      from: Date.parse('2026-09-28T03:30:00Z'),
      to: Date.parse('2026-09-28T05:00:00Z'),
      sure: { from: Date.parse('2026-09-28T03:31:00Z'), to: Date.parse('2026-09-28T04:00:00Z') },
    })
    expect(updates[0]?.detail?.map(plain)).toEqual([
      '28 Sept 2026',
      '03:31 – 04:00–05:00',
      '29 mins – 1 hr, 30 mins',
      'Version 2026.38.3.2',
    ])
  })

  it('says parked only maybe while a page of the events could not be read', async () => {
    mocks.fetchTrips.mockImplementation((_v: string, q: { cursor?: string }) =>
      q.cursor ? Promise.reject(new Error('down')) : Promise.resolve(trips1),
    )
    const w = await mounted()
    const bands = w.findComponent({ name: 'ChartFrame' }).props('bands') as Band[]
    const parked = bands.filter((b) => b.label === 'Parked')
    expect(parked).not.toHaveLength(0)
    expect(parked.every((b) => b.sure === undefined)).toBe(true)
  })

  it('says parked only maybe when the charges could not be read at all', async () => {
    mocks.fetchCharges.mockRejectedValue(new Error('down'))
    const w = await mounted()
    const bands = w.findComponent({ name: 'ChartFrame' }).props('bands') as Band[]
    const parked = bands.filter((b) => b.label === 'Parked')
    expect(parked).not.toHaveLength(0)
    expect(parked.every((b) => b.sure === undefined)).toBe(true)
  })

  it("tells each day's lowest and highest state of charge, and its events", async () => {
    const w = await mounted(true)
    const rows = w.findAll('tbody tr').map((r) => r.findAll('th, td').map((c) => plain(c.text())))
    expect(rows).toHaveLength(8)
    expect(rows.at(-1)).toEqual(['28 Sept 2026', '47%', '50%', '3', '2'])
    expect(rows[0]).toEqual(['21 Sept 2026', 'Unknown', 'Unknown', '0', '0'])
  })

  it('says so when nothing was read', async () => {
    mocks.fetchSeries.mockResolvedValue({ ...series, runs: [] })
    const w = await mounted()
    expect(w.find('.empty').text()).toBe('Nothing was read in the last 24 hours.')
    expect(w.find('canvas').exists()).toBe(false)
  })
})
