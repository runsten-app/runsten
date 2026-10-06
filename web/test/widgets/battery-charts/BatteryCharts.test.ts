import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { vuetify } from '@/app/providers/vuetify'
import type { Battery } from '@/entities/battery'
import { ChartFrame } from '@/shared/ui/chart'
import { BatteryCharts } from '@/widgets/battery-charts'
import battery from '@fixtures/battery.json'
import { mountWith, testRouter } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const { getBattery } = vi.hoisted(() => ({ getBattery: vi.fn() }))
vi.mock('@/entities/battery', async (original) => ({
  ...(await original<typeof import('@/entities/battery')>()),
  getBattery,
}))

beforeEach(() => {
  getBattery.mockReset()
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()
// The instants of the fixture, as milliseconds: the x axis is linear.
const x = (iso: string) => Date.parse(iso)

async function charts(body: unknown = battery, query = '') {
  getBattery.mockResolvedValue(body)
  const router = testRouter()
  await router.push(`/vehicles/v1/battery${query}`)
  const { wrapper } = mountWith(BatteryCharts, { router, props: { vehicle: 'v1' } })
  await flushPromises()
  return { wrapper, router }
}

type Dataset = {
  label: string
  data: unknown
  backgroundColor: string
  borderColor: string
  type?: string
  fill: string | false
  borderDash?: number[]
}
function datasets(w: VueWrapper, chart: string): Dataset[] {
  const c = w.find(`.${chart}`).findComponent({ name: 'Line' })
  return (c.props('data') as { datasets: Dataset[] }).datasets
}

describe('BatteryCharts', () => {
  it('draws the estimates of both sources, the monthly median, its band and the reference', async () => {
    const { wrapper: w } = await charts()
    expect(w.findAll('h2').map((h) => h.text())).toEqual([
      'Estimated capacity',
      "Range at 100 % (car's estimate)",
    ])
    const d = datasets(w, 'capacity')
    // The band is one series twice under one label: the legend shows it once, the table
    // keeps both quartiles (ChartFrame keeps the first of a shared label).
    const band = 'Middle half of the estimates (Q1–Q3)'
    expect(d.map((s) => [s.label, s.type])).toEqual([
      ['Charging power', 'scatter'],
      ['Billed energy', 'scatter'],
      [band, undefined],
      [band, undefined],
      ['Monthly median', undefined],
      ['Reference', undefined],
    ])
    // Each source keeps its points: the two estimates of one charge are two points,
    // never averaged. The charging power comes first, the billed energy second.
    expect((d[0]?.data as unknown[]).slice(0, 3)).toEqual([
      { x: x('2025-08-05T12:00:00Z'), y: 76 },
      { x: x('2025-08-12T12:00:00Z'), y: 76 },
      { x: x('2025-08-20T12:00:00Z'), y: 76 },
    ])
    expect((d[0]?.data as unknown[]).length).toBe(35)
    expect((d[1]?.data as unknown[]).length).toBe(11)
    // The sources are told apart by their color too.
    expect(d[0]?.backgroundColor).not.toBe(d[1]?.backgroundColor)
    // The band is filled towards the previous series, the first quartile.
    expect(d[2]?.fill).toBe(false)
    expect(d[3]?.fill).toBe('-1')
    expect(d[3]?.backgroundColor).toBe(vuetify.theme.themes.value.light?.colors['chart-band'])
    // The reference is dashed, not data of its own, and spans what is drawn.
    expect(d[5]?.borderDash).toEqual([6, 4])
    expect(d[5]?.data).toEqual([
      { x: x('2025-08-05T12:00:00Z'), y: 75 },
      { x: x('2026-09-20T12:00:00Z'), y: 75 },
    ])
    // A month without an estimate is a hole in the median, never a zero.
    expect((d[4]?.data as { x: number; y: number | null }[]).slice(0, 5)).toEqual([
      { x: x('2025-08-01T00:00:00Z'), y: 76 },
      { x: x('2025-09-01T00:00:00Z'), y: 75.75 },
      { x: x('2025-10-01T00:00:00Z'), y: null },
      { x: x('2025-11-01T00:00:00Z'), y: null },
      { x: x('2025-12-01T00:00:00Z'), y: 75 },
    ])
  })

  it('sums the capacity up in one sentence, for a screen reader, and its months in a table', async () => {
    const { wrapper: w } = await charts()
    expect(plain(w.find('.capacity canvas').attributes('aria-label'))).toBe(
      'Estimated capacity from 76 kWh in Aug 2025 to 72.8 kWh in Sept 2026.',
    )
    // The table gives the months, not the points one by one.
    expect(w.findAll('.capacity thead th').map((h) => h.text())).toEqual([
      'Month',
      'Median',
      'First quartile',
      'Third quartile',
      'Estimates',
    ])
    const rows = w
      .findAll('.capacity tbody tr')
      .map((r) => r.findAll('th, td').map((c) => plain(c.text())))
    expect(rows[0]).toEqual(['Aug 2025', '76 kWh', '76 kWh', '76.3 kWh', '4'])
    // An empty month is told unknown, its count zero: a hole, never a zero.
    expect(rows[2]).toEqual(['Oct 2025', 'Unknown', 'Unknown', 'Unknown', '0'])
    expect(rows).toHaveLength(14)
  })

  it('leads from a point to its charge, and only from a point', async () => {
    const { wrapper: w, router } = await charts()
    const frames = w.findAllComponents(ChartFrame)
    expect(frames.map((f) => f.props('nearest'))).toEqual([true, false])
    expect(frames.map((f) => f.props('hint'))).toEqual([
      'Select a point to see its charge.',
      "The car's own estimate, based on your recent driving: it drops in winter, whatever the battery.",
    ])
    // The first power point: its charge.
    frames[0]?.vm.$emit('select', 0, 0)
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/vehicles/v1/charges/2025-08-05T11:59:00.000000Z')
    // The first billed point: another charge.
    await router.push('/vehicles/v1/battery')
    frames[0]?.vm.$emit('select', 0, 1)
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/vehicles/v1/charges/2025-08-20T11:59:00.000000Z')
    // The lines are not points: nothing.
    await router.push('/vehicles/v1/battery')
    frames[0]?.vm.$emit('select', 0, 2)
    frames[0]?.vm.$emit('select', 0, 4)
    frames[0]?.vm.$emit('select', 99, 1) // no such point
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('battery')
  })

  it('follows what it draws on the y axis, and ticks the months once each', async () => {
    const { wrapper: w } = await charts()
    const [capacity, range] = w.findAllComponents(ChartFrame)
    // A level near 75 on an axis from zero would read flat: the axis hugs the values,
    // the reference included, rounded to the axis's step and outwards, so the extreme
    // labels read as round numbers. The statistics keep their zero.
    expect(capacity?.props('beginAtZero')).toBe(false)
    expect(capacity?.props('yMin')).toBe(72)
    expect(capacity?.props('yMax')).toBe(78)
    expect(range?.props('beginAtZero')).toBe(false)
    expect(range?.props('yMin')).toBe(380)
    expect(range?.props('yMax')).toBe(430)
    // The x axis stays within the months of the response.
    const starts = (battery.months as Battery['months']).map((m) => Date.parse(m.start))
    const ends = (battery.months as Battery['months']).map((m) => Date.parse(m.end))
    expect(capacity?.props('xMin')).toBe(starts[0])
    expect(capacity?.props('xMax')).toBe(ends.at(-1))
    // One tick per month at most: fourteen months of history, one in two.
    expect(capacity?.props('xTicks')).toEqual(starts.filter((_, i) => i % 2 === 0))
    expect(range?.props('xTicks')).toEqual(starts.filter((_, i) => i % 2 === 0))
  })

  it('shows a sentence instead of an empty capacity chart, keeping the title and the table', async () => {
    const b = structuredClone(battery) as Battery
    b.estimates = []
    for (const m of b.months) m.capacity = null
    const { wrapper: w } = await charts(b)
    // No canvas pretending to show something, no axis running to the years around
    // nothing: the sentence says what an estimate takes.
    expect(w.find('.capacity canvas').exists()).toBe(false)
    expect(w.find('.capacity .empty').text()).toBe(
      'No estimate yet: it takes a charge of at least 20 %.',
    )
    expect(w.find('.capacity h2').text()).toBe('Estimated capacity')
    expect(w.find('.capacity thead th').text()).toBe('Month')
    expect(w.findAll('.capacity tbody tr')).toHaveLength(14)
    // No estimate at all: no axis to pick either.
    expect(w.find('.axis-toggle').exists()).toBe(false)
    // The range chart has values: its canvas stays.
    expect(w.find('.range canvas').exists()).toBe(true)
  })

  it('shows a sentence instead of a range chart without any reading', async () => {
    const b = structuredClone(battery) as Battery
    for (const m of b.months) m.range_at_full = null
    const { wrapper: w } = await charts(b)
    expect(w.find('.range canvas').exists()).toBe(false)
    expect(w.find('.range .empty').text()).toBe('No range at 100 % yet.')
    expect(w.find('.range thead th').text()).toBe('Month')
    expect(w.find('.capacity canvas').exists()).toBe(true)
  })

  it('plots by mileage when the URL asks for it, leaving out what has no odometer', async () => {
    const { wrapper: w, router } = await charts(battery, '?axis=odometer')
    expect(router.currentRoute.value.query).toEqual({ axis: 'odometer' })
    const d = datasets(w, 'capacity')
    // The mileage axis has no months: no median, no band. The reference stays, against
    // the capacity it compares with.
    expect(d.map((s) => s.label)).toEqual(['Charging power', 'Billed energy', 'Reference'])
    // Only the estimates an odometer can place: 24 of the 35 power ones, the 11 billed.
    expect((d[0]?.data as unknown[]).length).toBe(24)
    expect((d[0]?.data as unknown[]).slice(0, 2)).toEqual([
      { x: 20040, y: 76 },
      { x: 20160, y: 76 },
    ])
    expect((d[1]?.data as unknown[]).length).toBe(11)
    // The ticks are kilometers, not raw numbers.
    const frame = w.findAllComponents(ChartFrame)[0]
    expect(frame?.props('formatX')?.(20040)).toBe('20,040 km')
    // The summary says what the axis left out, and the table still gives the months.
    expect(plain(w.find('.capacity canvas').attributes('aria-label'))).toBe(
      'Estimated capacity from 76 kWh at 20,040 km to 73.8 kWh at 26,660 km. 11 estimates without an odometer are not shown.',
    )
    expect(w.findAll('.capacity thead th').map((h) => h.text())).toEqual([
      'Month',
      'Median',
      'First quartile',
      'Third quartile',
      'Estimates',
    ])
  })

  it('says when no estimate can sit on the mileage axis', async () => {
    const b = structuredClone(battery) as Battery
    for (const e of b.estimates) e.odometer_km = null
    const { wrapper: w } = await charts(b, '?axis=odometer')
    // No empty frame: the sentence, with the title and the table kept.
    expect(w.find('.capacity canvas').exists()).toBe(false)
    expect(plain(w.find('.capacity .empty').text())).toBe('No estimate has an odometer.')
    expect(w.find('.capacity h2').text()).toBe('Estimated capacity')
    // The axis can still be changed back: the toggle stays.
    expect(w.find('.axis-toggle').exists()).toBe(true)
  })

  it('draws the displayed range at a full charge, with its holes and its summary', async () => {
    const { wrapper: w } = await charts()
    const d = datasets(w, 'range')
    expect(d).toHaveLength(1)
    expect(d[0]?.label).toBe('Range at 100 %')
    // A month without a reading is a hole, never a zero.
    expect((d[0]?.data as { x: number; y: number | null }[]).slice(0, 5)).toEqual([
      { x: x('2025-08-01T00:00:00Z'), y: 402 },
      { x: x('2025-09-01T00:00:00Z'), y: 402 },
      { x: x('2025-10-01T00:00:00Z'), y: 402 },
      { x: x('2025-11-01T00:00:00Z'), y: null },
      { x: x('2025-12-01T00:00:00Z'), y: 402 },
    ])
    expect(plain(w.find('.range canvas').attributes('aria-label'))).toBe(
      'Range at 100 % from 402 km in Aug 2025 to 402 km in Sept 2026.',
    )
    expect(w.findAll('.range thead th').map((h) => h.text())).toEqual(['Month', 'Median', 'Trips'])
    const rows = w.findAll('.range tbody tr').map((r) => r.findAll('th, td').map((c) => c.text()))
    expect(rows[0]).toEqual(['Aug 2025', '402 km', '2'])
    expect(rows[3]).toEqual(['Nov 2025', 'Unknown', '0'])
    // Nothing to press on this chart: no cursor of selection.
    expect(w.findAllComponents(ChartFrame)[1]?.props('selectable')).toBe(false)
  })

  it('says one month of estimate only, for a screen reader', async () => {
    const b = structuredClone(battery) as Battery
    // One month of history: the summaries name its figures alone.
    b.months = b.months.filter((_, i) => i === 0)
    const { wrapper: one } = await charts(b)
    expect(plain(one.find('.capacity canvas').attributes('aria-label'))).toBe(
      'Estimated capacity of 76 kWh in Aug 2025.',
    )
    expect(plain(one.find('.range canvas').attributes('aria-label'))).toBe(
      'Range at 100 % of 402 km in Aug 2025.',
    )
    // One month: one tick, and the axis of the month alone.
    const [capacity] = one.findAllComponents(ChartFrame)
    expect(capacity?.props('xTicks')).toEqual([x('2025-08-01T00:00:00Z')])
    expect(capacity?.props('xMin')).toBe(x('2025-08-01T00:00:00Z'))
    expect(capacity?.props('xMax')).toBe(x('2025-09-01T00:00:00Z'))
  })

  it('draws nothing without a month of history', async () => {
    const b = structuredClone(battery) as Battery
    b.months = []
    const { wrapper: w } = await charts(b)
    expect(w.find('.battery-charts').exists()).toBe(false)
    expect(w.findComponent(ChartFrame).exists()).toBe(false)
  })
})
