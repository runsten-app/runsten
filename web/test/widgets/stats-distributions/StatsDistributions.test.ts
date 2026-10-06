import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import type { Stats } from '@/entities/stats'
import { statsViews, type StatsView } from '@/features/view-stats'
import { i18n } from '@/shared/i18n'
import { ChartFrame } from '@/shared/ui/chart'
import { StatsDistributions } from '@/widgets/stats-distributions'
import empty from '@fixtures/stats-empty.json'
import stats from '@fixtures/stats.json'
import { mountWith } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const { fetchStats } = vi.hoisted(() => ({ fetchStats: vi.fn() }))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats,
}))

beforeEach(() => {
  fetchStats.mockReset()
})
afterEach(() => {
  i18n.global.locale.value = 'en'
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

// AllViews draws the distributions of every view, in their order: each test looks at
// one chart whatever its view; the views themselves are tested apart.
const AllViews = defineComponent({
  props: { vehicle: { type: String, required: true } },
  setup: (p) => () =>
    statsViews.map((view) =>
      h(StatsDistributions, { vehicle: p.vehicle, bucket: 'day', view, key: view }),
    ),
})

async function mounted(s: unknown) {
  fetchStats.mockResolvedValue(s)
  const { wrapper } = mountWith(AllViews, { props: { vehicle: 'v1' } })
  await flushPromises()
  return wrapper
}

type Dataset = { label: string; data: (number | null)[] }
function datasets(w: Awaited<ReturnType<typeof mounted>>, chart: string): Dataset[] {
  const c = w.find(`.${chart}`).findComponent({ name: 'Bar' })
  return (c.props('data') as { datasets: Dataset[] }).datasets
}

// withSoC is the fixture with these charges, by the points they started and ended at.
function withSoC(pairs: [number, number][], leftOut = 0): Stats {
  const s = structuredClone(stats) as Stats
  s.charges_by_soc = { start: Array(101).fill(0), end: Array(101).fill(0), left_out: leftOut }
  for (const [a, b] of pairs) {
    s.charges_by_soc.start[a] = (s.charges_by_soc.start[a] ?? 0) + 1
    s.charges_by_soc.end[b] = (s.charges_by_soc.end[b] ?? 0) + 1
  }
  return s
}

describe('StatsDistributions', () => {
  it('draws the distributions of its view only', async () => {
    fetchStats.mockResolvedValue(stats)
    const titles = async (view: StatsView) => {
      const { wrapper } = mountWith(StatsDistributions, {
        props: { vehicle: 'v1', bucket: 'day', view },
      })
      await flushPromises()
      return wrapper.findAll('h2').map((h) => h.text())
    }
    expect(await titles('driving')).toEqual([
      'Trips by distance',
      'Consumption by trip distance (estimated)',
    ])
    expect(await titles('charging')).toEqual([
      'State of charge at the start and end of charges',
      'Energy charged by place (from the state of charge)',
    ])
    expect(await titles('costs')).toEqual(['Cost of charges by place'])
  })

  it('draws the trips by band of distance, and what each band consumed', async () => {
    const w = await mounted(stats)
    expect(w.findAll('h2').map((h) => h.text())).toEqual([
      'Trips by distance',
      'Consumption by trip distance (estimated)',
      'State of charge at the start and end of charges',
      'Energy charged by place (from the state of charge)',
      'Cost of charges by place',
    ])
    expect(w.findAll('.by-distance tbody th').map((h) => plain(h.text()))).toEqual([
      'under 5 km',
      '5 to 20 km',
      '20 to 50 km',
      '50 to 100 km',
      '100 km and more',
    ])
    expect(datasets(w, 'by-distance')[0]?.data).toEqual([0, 0, 2, 0, 0])
    // A band without a known consumption has no bar, never a zero.
    expect(datasets(w, 'by-distance-consumption')[0]?.data).toEqual([null, null, 18.75, null, null])
    expect(plain(w.find('.by-distance canvas').attributes('aria-label'))).toBe(
      'Trips by distance: most often 20 to 50 km (2 of 2).',
    )
    expect(plain(w.find('.by-distance-consumption canvas').attributes('aria-label'))).toBe(
      'Consumption by trip distance: 20 to 50 km, 18.8 kWh/100 km.',
    )
    // The reconstructed trip is in no band: said under the chart.
    expect(plain(w.find('.by-distance .hint').text())).toBe(
      '1 trip left out: reconstructed, which may hold several, or without a distance.',
    )
    // No list filters by band: a press leads nowhere.
    const frames = w.findAllComponents(ChartFrame)
    expect(frames.map((f) => f.props('selectable'))).toEqual([false, false, false, false, false])
  })

  it('draws the charges by the state of charge they started and ended at', async () => {
    const w = await mounted(
      withSoC(
        [
          [19, 80],
          [20, 81],
          [47, 100],
          [5, 95],
        ],
        2,
      ),
    )
    expect(w.findAll('.by-soc tbody th').map((h) => plain(h.text()))).toEqual([
      '0–10%',
      '10–20%',
      '20–30%',
      '30–40%',
      '40–50%',
      '50–60%',
      '60–70%',
      '70–80%',
      '80–90%',
      '90–100%',
    ])
    expect(datasets(w, 'by-soc').map((d) => [d.label, d.data])).toEqual([
      ['Started at', [1, 1, 1, 0, 1, 0, 0, 0, 0, 0]],
      // 100 % lies in the last band.
      ['Ended at', [0, 0, 0, 0, 0, 0, 0, 0, 2, 2]],
    ])
    // From the points: 20 % is not below 20 %, nor 80 % above 80 %.
    expect(plain(w.find('.by-soc canvas').attributes('aria-label'))).toBe(
      'Of 4 charges, 2 started below 20% and 3 ended above 80%.',
    )
    expect(plain(w.find('.by-soc .hint').text())).toBe(
      'Each charge counts twice: at the state of charge it started at, and at the one it ended at. ' +
        '2 charges left out: reconstructed, or without a state of charge.',
    )
  })

  it('draws the energy and the cost of the charges by place', async () => {
    const s = structuredClone(stats) as Stats
    const group = (
      count: number,
      kwh: number,
      cost: [number, number] | null,
      reconstructed = 0,
    ) => ({
      count,
      reconstructed,
      energy_soc_kwh: kwh,
      energy_soc_unknown: 0,
      cost: { min_minor: cost?.[0] ?? 0, max_minor: cost?.[1] ?? 0 },
      cost_unknown: cost ? 0 : count,
      cost_entered: 0,
    })
    s.charges_by_place = {
      places: [
        { place: { id: 'w', name: 'Work' }, ...group(1, 8, [150, 150]) },
        { place: { id: 'h', name: 'Home' }, ...group(3, 40, [500, 650], 1) },
      ],
      outside: { ac: group(0, 0, null), dc: group(1, 35, null), unknown: group(0, 0, null) },
      no_position: { ...group(1, 12, null, 1), energy_soc_unknown: 1 },
    }
    const w = await mounted(s)
    // The places, the most energy first, then the groups that hold a charge.
    expect(w.findAll('.by-place tbody th').map((h) => plain(h.text()))).toEqual([
      'Home',
      'Work',
      'Elsewhere, DC',
      'No position',
    ])
    // A group whose energy is unknown has no bar; the table counts the charges.
    expect(datasets(w, 'by-place')[0]?.data).toEqual([40, 8, 35, null])
    expect(w.findAll('.by-place thead th').map((h) => plain(h.text()))).toEqual([
      'Place',
      'Energy',
      'Charges',
    ])
    expect(
      w
        .findAll('.by-place tbody tr')[0]
        ?.findAll('td')
        .map((c) => plain(c.text())),
    ).toEqual(['40 kWh', '3'])
    expect(plain(w.find('.by-place canvas').attributes('aria-label'))).toBe(
      'Energy charged by place: 83 kWh in all, the most for Home (40 kWh).',
    )
    expect(plain(w.find('.by-place .hint').text())).toContain(
      '2 reconstructed charges included, counted the same way.',
    )
    // The lowest cost and its uncertainty; unknown outside the places.
    expect(datasets(w, 'cost-by-place').map((d) => d.data)).toEqual([
      [5, 1.5, null, null],
      [1.5, 0, null, null],
    ])
    expect(plain(w.find('.cost-by-place canvas').attributes('aria-label'))).toBe(
      'Cost of charges by place: Home, from €5.00 to €6.50; Work, €1.50.',
    )
  })

  it('draws no cost by place without a currency', async () => {
    const w = await mounted({ ...structuredClone(stats), currency: null })
    expect(w.find('.by-place canvas').exists()).toBe(true)
    expect(w.find('.cost-by-place').exists()).toBe(false)
  })

  it('says so when the period has nothing to spread', async () => {
    const w = await mounted(empty)
    expect(w.findAll('.empty').map((p) => plain(p.text()))).toEqual([
      'No trip with a known distance in this period.',
      'No consumption known in this period.',
      'No charge with a known state of charge in this period.',
      'No charge with a known energy in this period.',
      'No known cost in this period.',
    ])
    expect(w.find('canvas').exists()).toBe(false)
    expect(w.find('.by-distance .hint').exists()).toBe(false)
  })

  it('names the bands in the reader’s language', async () => {
    i18n.global.locale.value = 'fr'
    const w = await mounted(stats)
    expect(w.findAll('.by-distance tbody th').map((h) => plain(h.text()))).toEqual([
      'moins de 5 km',
      '5 à 20 km',
      '20 à 50 km',
      '50 à 100 km',
      '100 km et plus',
    ])
    expect(plain(w.find('.by-soc tbody th').text())).toBe('0–10 %')
  })
})
