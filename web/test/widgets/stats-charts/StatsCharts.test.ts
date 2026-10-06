import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import type { Stats } from '@/entities/stats'
import { statsViews, type StatsView } from '@/features/view-stats'
import { ChartFrame } from '@/shared/ui/chart'
import { StatsCharts } from '@/widgets/stats-charts'
import empty from '@fixtures/stats-empty.json'
import stats from '@fixtures/stats.json'
import { mountWith, testRouter } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const { fetchStats } = vi.hoisted(() => ({ fetchStats: vi.fn() }))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats,
}))

beforeEach(() => {
  fetchStats.mockReset()
})

const plain = (s = '') => s.replace(/\s+/g, ' ')

// AllViews draws the charts of every view, in their order: each test looks at one chart
// whatever its view; the views themselves are tested apart.
const AllViews = defineComponent({
  props: { vehicle: { type: String, required: true }, bucket: { type: String, required: true } },
  setup: (p) => () =>
    statsViews.map((view) =>
      h(StatsCharts, { vehicle: p.vehicle, bucket: p.bucket as 'day', view, key: view }),
    ),
})

async function charts(s: unknown, bucket = 'day') {
  fetchStats.mockResolvedValue(s)
  const { wrapper } = mountWith(AllViews, { props: { vehicle: 'v1', bucket } })
  await flushPromises()
  return wrapper
}

type Dataset = { label: string; data: (number | null)[]; backgroundColor: string }
function datasets(w: Awaited<ReturnType<typeof charts>>, chart: string): Dataset[] {
  const c = w.find(`.${chart}`).findComponent({
    name: ['consumption', 'cost-per-distance', 'parked'].includes(chart) ? 'Line' : 'Bar',
  })
  return (c.props('data') as { datasets: Dataset[] }).datasets
}

describe('StatsCharts', () => {
  it('draws the charts of its view only', async () => {
    fetchStats.mockResolvedValue(stats)
    const titles = async (view: StatsView) => {
      const { wrapper } = mountWith(StatsCharts, { props: { vehicle: 'v1', bucket: 'day', view } })
      await flushPromises()
      return wrapper.findAll('h2').map((h) => h.text())
    }
    expect(await titles('driving')).toEqual([
      'Distance',
      'Consumption (estimated)',
      'Time driving and charging',
    ])
    expect(await titles('charging')).toEqual([
      'Energy charged (from the state of charge)',
      'Loss while parked',
    ])
    expect(await titles('costs')).toEqual(['Cost of charges', 'Cost per 100 km'])
  })

  it('draws the distance, the energy by type and the consumption of each interval', async () => {
    const w = await charts(stats)
    expect(w.findAll('h2').map((h) => h.text())).toEqual([
      'Distance',
      'Consumption (estimated)',
      'Time driving and charging',
      'Energy charged (from the state of charge)',
      'Loss while parked',
      'Cost of charges',
      'Cost per 100 km',
    ])
    // The intervals start at midnight in Paris: 22:00 UTC the day before, here in UTC.
    expect(w.find('.distance tbody th').text()).toBe('27 Sept')
    expect(w.find('.distance thead th').text()).toBe('Day')
    expect(datasets(w, 'distance')[0]?.data).toEqual([73, 0, 0])
    // The fixture's reconstructed charge has no type: a third series, so the bars add up.
    expect(datasets(w, 'energy').map((d) => [d.label, d.data])).toEqual([
      ['AC', [26.4, 0, 0]],
      ['DC', [0, 0, 0]],
      ['Unknown type', [17.6, 0, 0]],
    ])
    // AC and DC told apart by their color too.
    const [ac, dc] = datasets(w, 'energy')
    expect(ac?.backgroundColor).not.toBe(dc?.backgroundColor)
    // A gap where the consumption is unknown, never a zero.
    expect(datasets(w, 'consumption')[0]?.data).toEqual([18.75, null, null])
  })

  it('sums each chart up in one sentence, for a screen reader', async () => {
    const w = await charts(stats)
    const label = (c: string) => plain(w.find(`.${c} canvas`).attributes('aria-label'))
    expect(label('distance')).toBe('Distance: 73 km in all, the most for 27 Sept (73 km).')
    expect(label('energy')).toBe('Energy charged: 26 kWh in AC, 0 kWh in DC.')
    expect(label('consumption')).toBe(
      'Consumption: 18.8 kWh/100 km on average, from 18.8 kWh/100 km to 18.8 kWh/100 km.',
    )
  })

  it('says so when a period has nothing to draw', async () => {
    const w = await charts(empty, 'week')
    const label = (c: string) => plain(w.find(`.${c} canvas`).attributes('aria-label'))
    expect(label('distance')).toBe('No distance in this period.')
    expect(label('energy')).toBe('No charge in this period.')
    expect(label('consumption')).toBe('No consumption known in this period.')
    expect(datasets(w, 'energy')).toHaveLength(2)
    expect(w.find('.distance thead th').text()).toBe('Week from')
  })

  it('draws nothing without intervals', async () => {
    const s = structuredClone(stats) as Stats
    s.buckets = []
    const w = await charts(s)
    expect(w.find('.stats-charts').exists()).toBe(false)
  })

  it('leads from an interval to its events, in the list of trips or of charges', async () => {
    fetchStats.mockResolvedValue(stats)
    const router = testRouter()
    await router.push('/vehicles/v1/stats')
    const { wrapper } = mountWith(AllViews, { router, props: { vehicle: 'v1', bucket: 'day' } })
    await flushPromises()
    const all = wrapper.findAllComponents(ChartFrame)
    const order = [
      'distance',
      'energy',
      'consumption',
      'time',
      'parked',
      'cost',
      'cost-per-distance',
    ]
    const frames = order.map((c) => all.find((f) => f.classes(c)))
    expect(frames.map((f) => f?.props('hint'))).toEqual([
      'Select a bar to see the trips of its interval.',
      'Select a bar to see the charges of its interval.',
      'Select a point to see the trips of its interval.',
      'Select a bar to see the trips of its interval.',
      'The state of charge lost between events, per day: meaningful over weeks, not over a night.',
      'Select a bar to see the charges of its interval.',
      'Select a point to see the charges of its interval.',
    ])
    // The second interval: 28 September in Paris, 22:00 to 22:00 UTC; its days here, in UTC.
    frames[0]?.vm.$emit('select', 1)
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe(
      '/vehicles/v1/trips?from=2026-09-28&to=2026-09-29',
    )
    frames[1]?.vm.$emit('select', 0)
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('charges')
    expect(router.currentRoute.value.query).toEqual({ from: '2026-09-27', to: '2026-09-28' })
    frames[2]?.vm.$emit('select', 9) // no such interval
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('charges')
    await router.push('/vehicles/v1/stats')
    frames[3]?.vm.$emit('select', 0)
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('trips')
    await router.push('/vehicles/v1/stats')
    frames[6]?.vm.$emit('select', 2)
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('charges')
    expect(router.currentRoute.value.query).toEqual({ from: '2026-09-29', to: '2026-09-30' })
  })

  it('stacks the lowest cost of each interval and its uncertainty, in amounts', async () => {
    const w = await charts(stats)
    expect(datasets(w, 'cost').map((d) => [d.label, d.data])).toEqual([
      ['Lowest cost', [13.74, 0, 0]],
      ['Uncertainty, up to the highest cost', [0.55, 0, 0]],
    ])
    const [min, gap] = datasets(w, 'cost')
    expect(min?.backgroundColor).not.toBe(gap?.backgroundColor)
    const frame = w.findAllComponents(ChartFrame).at(5)
    expect(frame?.props('stacked')).toBe(true)
    expect(frame?.props('unit')).toBe('€')
    expect(frame?.props('format')(null)).toBe('Unknown')
    // The table gives the bounds themselves, not their difference.
    expect(w.findAll('.cost thead th').map((h) => h.text())).toEqual([
      'Day',
      'Lowest cost',
      'Highest cost',
    ])
    expect(
      w
        .findAll('.cost tbody tr')
        .at(0)
        ?.findAll('td')
        .map((d) => d.text()),
    ).toEqual(['€13.74', '€14.29'])
    expect(plain(w.find('.cost canvas').attributes('aria-label'))).toBe(
      'Cost of charges: from €13.74 to €14.29 in all, the most for 27 Sept (from €13.74 to €14.29).',
    )
  })

  it('leaves a gap where every cost of an interval is unknown, never a zero', async () => {
    const s = structuredClone(stats) as Stats
    const first = s.buckets[0]?.charges
    if (first) first.cost_unknown = first.count
    s.totals.charges.cost_unknown = s.totals.charges.count
    const w = await charts(s)
    expect(datasets(w, 'cost').map((d) => d.data)).toEqual([
      [null, 0, 0],
      [null, 0, 0],
    ])
    expect(
      w
        .findAll('.cost tbody tr')
        .at(0)
        ?.findAll('td')
        .map((d) => d.text()),
    ).toEqual(['Unknown', 'Unknown'])
    expect(plain(w.find('.cost canvas').attributes('aria-label'))).toBe(
      'No known cost in this period.',
    )
  })

  it('sums free charges up without a peak', async () => {
    const s = structuredClone(stats) as Stats
    for (const b of s.buckets) b.charges.cost = { min_minor: 0, max_minor: 0 }
    s.totals.charges.cost = { min_minor: 0, max_minor: 0 }
    const w = await charts(s)
    expect(plain(w.find('.cost canvas').attributes('aria-label'))).toBe(
      'Cost of charges: €0.00 in all.',
    )
  })

  it('draws no cost without a currency', async () => {
    const w = await charts({ ...stats, currency: null })
    expect(w.findAll('.chart-frame')).toHaveLength(5)
    expect(w.find('.cost').exists()).toBe(false)
    expect(w.find('.cost-per-distance').exists()).toBe(false)
  })

  it('draws what 100 km cost: the lowest, banded up to the highest', async () => {
    const w = await charts(stats)
    // 13.74 to 14.29 € over 73 km, the lowest rounded down and the highest up; no rate
    // without a distance.
    expect(datasets(w, 'cost-per-distance').map((d) => [d.label, d.data])).toEqual([
      ['Lowest cost', [18.82, null, null]],
      ['Range, up to the highest cost', [19.58, null, null]],
    ])
    const frame = w.findAllComponents(ChartFrame).at(6)
    expect(frame?.props('unit')).toBe('€/100 km')
    expect(frame?.props('series')[1]?.band).toBe(true)
    expect(
      w
        .findAll('.cost-per-distance tbody tr')
        .at(0)
        ?.findAll('td')
        .map((d) => d.text()),
    ).toEqual(['€18.82/100 km', '€19.58/100 km'])
    expect(
      w
        .findAll('.cost-per-distance tbody tr')
        .at(1)
        ?.findAll('td')
        .map((d) => d.text()),
    ).toEqual(['Unknown', 'Unknown'])
    expect(plain(w.find('.cost-per-distance canvas').attributes('aria-label'))).toBe(
      'Cost per 100 km: from €18.82 to €19.58 over the period.',
    )
  })

  it('has no cost per 100 km where a cost or a distance is unknown', async () => {
    const s = structuredClone(stats) as Stats
    const first = s.buckets[0]
    if (first) first.trips.distance_unknown = 1
    s.totals.charges.cost_unknown = 1
    const w = await charts(s)
    expect(datasets(w, 'cost-per-distance').map((d) => d.data)).toEqual([
      [null, null, null],
      [null, null, null],
    ])
    expect(plain(w.find('.cost-per-distance canvas').attributes('aria-label'))).toBe(
      'No cost per 100 km known in this period.',
    )
  })

  it('stacks the time driving and charging, each with its uncertainty, in hours', async () => {
    const w = await charts(stats)
    expect(datasets(w, 'time').map((d) => [d.label, d.data])).toEqual([
      ['Driving', [4860 / 3600, 0, 0]],
      ['Driving, uncertainty', [840 / 3600, 0, 0]],
      ['Charging', [12300 / 3600, 0, 0]],
      ['Charging, uncertainty', [660 / 3600, 0, 0]],
    ])
    const frame = w.findAllComponents(ChartFrame).find((f) => f.classes('time'))
    expect(frame?.props('stacked')).toBe(true)
    expect(frame?.props('unit')).toBe('h')
    expect(frame?.props('format')(null)).toBe('Unknown')
    expect(w.findAll('.time thead th').map((h) => h.text())).toEqual([
      'Day',
      'Driving, at least',
      'Driving, at most',
      'Charging, at least',
      'Charging, at most',
    ])
    expect(
      w
        .findAll('.time tbody tr')
        .at(0)
        ?.findAll('td')
        .map((d) => plain(d.text())),
    ).toEqual(['1 hr, 21 mins', '1 hr, 35 mins', '3 hrs, 25 mins', '3 hrs, 36 mins'])
    expect(plain(w.find('.time canvas').attributes('aria-label'))).toBe(
      'Time driving: 1 hr, 21 mins – 1 hr, 35 mins; charging: 3 hrs, 25 mins – 3 hrs, 36 mins.',
    )
  })

  it('leaves no bar where every duration of an interval is unknown', async () => {
    const s = structuredClone(stats) as Stats
    const first = s.buckets[0]
    if (first) {
      first.trips.driving_time_unknown = first.trips.count
      first.charges.charging_time_unknown = first.charges.count
    }
    const w = await charts(s)
    expect(datasets(w, 'time').map((d) => d.data[0])).toEqual([null, null, null, null])
  })

  it('says so when there is no time to draw', async () => {
    const w = await charts(empty, 'week')
    expect(plain(w.find('.time canvas').attributes('aria-label'))).toBe(
      'No trip or charge in this period.',
    )
  })

  it('draws the loss while parked per day, a gap where it is unknown, leading nowhere', async () => {
    const w = await charts(stats)
    expect(datasets(w, 'parked')[0]?.data).toEqual([0, null, null])
    const frame = w.findAllComponents(ChartFrame).at(4)
    expect(frame?.props('selectable')).toBe(false)
    expect(frame?.props('unit')).toBe('%/day')
    expect(frame?.props('format')(null)).toBe('Unknown')
    expect(plain(w.find('.parked tbody td').text())).toBe('0% per day')
    expect(plain(w.find('.parked canvas').attributes('aria-label'))).toBe(
      'Loss while parked: 0% per day on average over the period.',
    )
    const none = await charts(empty, 'week')
    expect(plain(none.find('.parked canvas').attributes('aria-label'))).toBe(
      'No loss while parked known in this period.',
    )
  })
})
