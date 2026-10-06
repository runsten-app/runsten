import { beforeEach, describe, expect, it, vi } from 'vitest'
import { vuetify } from '@/app/providers/vuetify'
import { ChartFrame } from '@/shared/ui/chart'
import { mountWith } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))

const format = (v: number | null) => (v === null ? 'Unknown' : `${v} km`)
const props = {
  title: 'Distance',
  summary: 'Distance: 30 km in all.',
  kind: 'bar',
  labels: ['1 Sept', '2 Sept'],
  series: [{ label: 'Distance', data: [10, null], color: '#1d5b8f' }],
  unit: 'km',
  intervalHeader: 'Day',
  format,
}

function mounted(p: Record<string, unknown> = {}) {
  return mountWith(ChartFrame, { props: { ...props, ...p } }).wrapper
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('ChartFrame', () => {
  it('is an image with its summary, under a heading', () => {
    const w = mounted()
    expect(w.find('h2').text()).toBe('Distance')
    const canvas = w.find('canvas')
    expect(canvas.attributes('role')).toBe('img')
    expect(canvas.attributes('aria-label')).toBe('Distance: 30 km in all.')
  })

  it('holds the same figures in a table for screen readers only, unknown told', () => {
    const w = mounted()
    const details = w.find('.d-sr-only')
    expect(details.find('caption').text()).toBe('Distance')
    expect(details.findAll('thead th').map((c) => c.text())).toEqual(['Day', 'Distance'])
    const rows = details.findAll('tbody tr').map((r) => r.findAll('th, td').map((c) => c.text()))
    expect(rows).toEqual([
      ['1 Sept', '10 km'],
      ['2 Sept', 'Unknown'],
    ])
  })

  it('draws with the series, a gap for the unknown, a legend for several', () => {
    const single = mounted()
    expect(single.find('.legend').exists()).toBe(false)
    const one = single.findComponent({ name: 'Bar' })
    const data = one.props('data') as { datasets: { data: unknown[]; spanGaps: boolean }[] }
    expect(data.datasets[0]?.data).toEqual([10, null])
    expect(data.datasets[0]?.spanGaps).toBe(false)
    const options = one.props('options') as Record<string, never>
    expect(options).toMatchObject({
      plugins: { legend: { display: false } },
      scales: { y: { beginAtZero: true, title: { text: 'km' }, stacked: false } },
    })

    // The legend is the page's: the series with their marks.
    const w = mounted({
      kind: 'line',
      stacked: true,
      series: [...props.series, { label: 'Other', data: [1, 2], color: '#a14f00' }],
    })
    expect(w.findAll('.legend li').map((l) => l.text())).toEqual(['Distance', 'Other'])
    expect(w.findAll('.legend .mark').map((m) => m.classes()[1])).toEqual([
      'mark--line',
      'mark--line',
    ])
    const two = w.findComponent({ name: 'Line' })
    expect(two.props('options')).toMatchObject({
      plugins: { legend: { display: false } },
      scales: { x: { stacked: true }, y: { stacked: true } },
    })
  })

  it('writes the values of the tooltip as the table does', () => {
    const bar = mounted().findComponent({ name: 'Bar' })
    const options = bar.props('options') as {
      plugins: { tooltip: { callbacks: { label: (c: unknown) => string } } }
    }
    const label = options.plugins.tooltip.callbacks.label
    expect(label({ dataset: { label: 'Distance' }, raw: 10 })).toBe('Distance: 10 km')
    expect(label({ dataset: {}, raw: null })).toBe(': Unknown')
  })

  // Horizontal rules only, in the theme's grid color; the ticks never turn.
  it('draws the rules of the values only, in the grid color', () => {
    const bar = mounted().findComponent({ name: 'Bar' })
    const options = bar.props('options') as {
      scales: {
        x: { grid: { display: boolean }; ticks: { maxRotation: number } }
        y: { grid: { color: string } }
      }
    }
    expect(options.scales.x.grid.display).toBe(false)
    expect(options.scales.x.ticks.maxRotation).toBe(0)
    expect(options.scales.y.grid.color).toBe(vuetify.theme.themes.value.light?.colors['chart-grid'])
  })

  // An interval measured at zero has a stub on the axis; an unknown one has nothing.
  it('marks a measured zero, never an unknown', () => {
    const bar = mounted({ series: [{ label: 'Distance', data: [0, null], color: '#1d5b8f' }] })
    const [zeros] = bar.findComponent({ name: 'Bar' }).props('plugins') as {
      afterDatasetsDraw: (chart: unknown) => void
    }[]
    const rects: number[][] = []
    const ctx = {
      save: () => undefined,
      restore: () => undefined,
      fillStyle: '',
      fillRect: (...r: number[]) => rects.push(r),
    }
    zeros?.afterDatasetsDraw({
      data: { datasets: [{ data: [0, null] }] },
      getDatasetMeta: () => ({
        hidden: false,
        data: [
          { x: 10, width: 6 },
          { x: 30, width: 6 },
        ],
      }),
      scales: { y: { getPixelForValue: () => 100 } },
      ctx,
    })
    expect(rects).toEqual([[7, 98, 6, 2]])
  })

  // Spans behind the series: faint where an event may have been, again where it surely
  // was, clipped to the chart's area; in the legend after the series, once per label.
  it('fills the bands behind the series, and names them in the legend', () => {
    const w = mounted({
      kind: 'line',
      formatX: String,
      series: [{ label: 'State of charge', data: [{ x: 0, y: 50 }], color: '#1d5b8f' }],
      bands: [
        { label: 'Driving', color: '#aa0000', from: -50, to: 40, sure: { from: 10, to: 30 } },
        { label: 'Driving', color: '#aa0000', from: 60, to: 60 },
      ],
    })
    expect(w.findAll('.legend li').map((l) => l.text())).toEqual(['State of charge', 'Driving'])
    expect(w.find('.legend .mark--span').exists()).toBe(true)
    const [spans] = w.findComponent({ name: 'Line' }).props('plugins') as {
      beforeDatasetsDraw: (chart: unknown) => void
    }[]
    const rects: number[][] = []
    const alphas: number[] = []
    const ctx = {
      save: () => undefined,
      restore: () => undefined,
      fillStyle: '',
      globalAlpha: 1,
      fillRect(...r: number[]) {
        rects.push(r)
        alphas.push(this.globalAlpha)
      },
    }
    spans?.beforeDatasetsDraw({
      scales: { x: { getPixelForValue: (v: number) => v } },
      chartArea: { left: 0, right: 100, top: 5, height: 50 },
      ctx,
    })
    // Clipped at the left, the sure part over it, and a pixel for an instant.
    expect(rects).toEqual([
      [0, 5, 40, 50],
      [10, 5, 20, 50],
      [60, 5, 1, 50],
    ])
    expect(alphas).toEqual([0.14, 0.14, 0.14])
  })

  it('draws a series on the right axis, in its own format', () => {
    const w = mounted({
      kind: 'line',
      formatX: String,
      formatPoint: (v: number) => `at ${v}`,
      rightUnit: '%',
      rightMin: 0,
      rightMax: 100,
      series: [
        { label: 'Power', data: [{ x: 0, y: 7.4 }], color: '#1d5b8f' },
        {
          label: 'State of charge',
          data: [{ x: 0, y: 50 }],
          color: '#aa0000',
          right: true,
          format: (v: number | null) => `${v} %`,
        },
      ],
    })
    const line = w.findComponent({ name: 'Line' })
    const data = line.props('data') as { datasets: { yAxisID: string }[] }
    expect(data.datasets.map((d) => d.yAxisID)).toEqual(['y', 'right'])
    const options = line.props('options') as {
      scales: { right: { position: string; min: number; max: number; title: { text: string } } }
      plugins: {
        tooltip: {
          callbacks: {
            label: (c: unknown) => string
            title: (items: { parsed: { x: number } }[]) => string
          }
        }
      }
    }
    expect(options.scales.right).toMatchObject({
      position: 'right',
      min: 0,
      max: 100,
      title: { text: '%' },
    })
    const { label, title } = options.plugins.tooltip.callbacks
    expect(
      label({ dataset: { label: 'State of charge' }, datasetIndex: 1, raw: { x: 0, y: 50 } }),
    ).toBe('State of charge: 50 %')
    expect(title([{ parsed: { x: 3 } }])).toBe('at 3')
    // Without a right unit, no right axis.
    const plainLine = mounted({ kind: 'line' }).findComponent({ name: 'Line' })
    expect((plainLine.props('options') as { scales: object }).scales).not.toHaveProperty('right')
  })

  it('begins the y axis at zero for sums, and follows the data for a level', () => {
    const sums = mounted().findComponent({ name: 'Bar' }).props('options') as {
      scales: { y: Record<string, unknown> }
    }
    expect(sums.scales.y).toMatchObject({ beginAtZero: true })
    // An estimated capacity near 75 on an axis from zero reads flat.
    const level = mounted({
      kind: 'line',
      labels: [],
      series: [{ label: 'Median', data: [{ x: 1, y: 75 }], color: '#1d5b8f' }],
      formatX: (v: number) => `${v}`,
      beginAtZero: false,
      yMin: 70,
      yMax: 80,
    }).findComponent({ name: 'Line' })
    const options = level.props('options') as { scales: { y: Record<string, unknown> } }
    expect(options.scales.y).toMatchObject({ beginAtZero: false, min: 70, max: 80 })
  })

  it('ticks the x axis where the widget says, never anywhere in a month', () => {
    const line = mounted({
      kind: 'line',
      labels: [],
      series: [{ label: 'Median', data: [{ x: 1, y: 75 }], color: '#1d5b8f' }],
      formatX: (v: number) => `${v}`,
      xMin: 1,
      xMax: 3,
      xTicks: [1, 2, 3],
    }).findComponent({ name: 'Line' })
    const options = line.props('options') as {
      scales: {
        x: { min?: number; max?: number; afterBuildTicks?: (s: { ticks: unknown }) => void }
      }
    }
    // The axis never leaves the history.
    expect(options.scales.x).toMatchObject({ min: 1, max: 3 })
    const scale = { ticks: [{ value: 99 }] }
    options.scales.x.afterBuildTicks?.(scale)
    expect(scale.ticks).toEqual([{ value: 1 }, { value: 2 }, { value: 3 }])
  })

  it('says a sentence instead of an empty canvas, keeping the title and the table', () => {
    const w = mounted({
      series: [{ label: 'Median', data: [], color: '#1d5b8f' }],
      hint: 'Select a point to see its charge.',
      empty: 'No estimate yet: it takes a charge of at least 20 %.',
    })
    // No empty frame pretending to show something, and no hint of a press.
    expect(w.find('canvas').exists()).toBe(false)
    expect(w.find('.empty').text()).toBe('No estimate yet: it takes a charge of at least 20 %.')
    expect(w.find('.hint').exists()).toBe(false)
    // The heading and the figures for screen readers stay.
    expect(w.find('h2').text()).toBe('Distance')
    expect(w.find('.d-sr-only table').exists()).toBe(true)
    expect(mounted().find('canvas').exists()).toBe(true)
  })

  it('shows a band once in the legend, not once per bound', () => {
    const w = mounted({
      series: [
        {
          label: 'Middle half of the estimates (Q1–Q3)',
          data: [1, 2],
          color: '#5b8fc4',
          edge: true,
        },
        {
          label: 'Middle half of the estimates (Q1–Q3)',
          data: [2, 3],
          color: '#5b8fc4',
          band: true,
        },
        { label: 'Monthly median', data: [1, 2], color: '#1d5b8f' },
      ],
    })
    expect(w.findAll('.legend li').map((l) => l.text())).toEqual([
      'Middle half of the estimates (Q1–Q3)',
      'Monthly median',
    ])
  })

  it('says what pressing an interval does, and selects its whole column', async () => {
    const w = mounted({ hint: 'Select a bar to see the trips of its interval.' })
    expect(w.find('.hint').text()).toBe('Select a bar to see the trips of its interval.')
    expect(mounted().find('.hint').exists()).toBe(false)

    const options = w.findComponent({ name: 'Bar' }).props('options') as {
      interaction: unknown
      onClick: (e: unknown, els: unknown, chart: unknown) => void
      onHover: (e: unknown, els: unknown[]) => void
    }
    expect(options.interaction).toEqual({ mode: 'index', intersect: false })
    const chart = (hits: { index: number; datasetIndex: number }[]) => ({
      getElementsAtEventForMode: () => hits,
    })
    options.onClick({}, [], chart([{ index: 1, datasetIndex: 0 }]))
    options.onClick({}, [], chart([])) // outside the intervals: nothing
    expect(w.findComponent(ChartFrame).emitted('select')).toEqual([[1, 0]])

    // A hand over an interval, the arrow elsewhere.
    const canvas = document.createElement('canvas')
    options.onHover({ native: { target: canvas } }, [{}])
    expect(canvas.style.cursor).toBe('pointer')
    options.onHover({ native: { target: canvas } }, [])
    expect(canvas.style.cursor).toBe('')
    options.onHover({}, [{}])
  })

  it('answers the nearest point over a linear axis, and only where a press leads', () => {
    // A scatter among the lines: the estimates of the capacity chart, by mileage.
    const w = mounted({
      kind: 'line',
      labels: [],
      series: [{ label: 'Points', data: [{ x: 20040, y: 76 }], color: '#1d5b8f', scatter: true }],
      formatX: (v: number) => `${v} km`,
      nearest: true,
    })
    const line = w.findComponent({ name: 'Line' })
    const data = line.props('data') as {
      datasets: { type?: string; pointHitRadius?: number; showLine?: boolean }[]
    }
    expect(data.datasets[0]?.type).toBe('scatter')
    expect(data.datasets[0]?.pointHitRadius).toBe(10)
    const options = line.props('options') as {
      interaction: unknown
      onClick: (e: unknown, els: unknown, chart: unknown) => void
      onHover: (e: unknown, els: unknown[]) => void
      plugins: { tooltip: { callbacks: { title?: (i: { parsed: { x: number } }[]) => string } } }
      scales: { x: { type?: string; ticks: { callback?: (v: number) => string } } }
    }
    expect(options.interaction).toEqual({ mode: 'nearest', intersect: true })
    const chart = (hits: { index: number; datasetIndex: number }[]) => ({
      getElementsAtEventForMode: () => hits,
    })
    options.onClick({}, [], chart([{ index: 0, datasetIndex: 0 }]))
    expect(w.findComponent(ChartFrame).emitted('select')).toEqual([[0, 0]])
    options.onClick({}, [], chart([])) // not on a point: nothing
    expect(w.findComponent(ChartFrame).emitted('select')).toHaveLength(1)

    // The ticks and the tooltip title are the reader's format, not raw milliseconds.
    expect(options.scales.x.type).toBe('linear')
    expect(options.scales.x.ticks.callback?.(20040)).toBe('20040 km')
    expect(options.plugins.tooltip.callbacks.title?.([{ parsed: { x: 20040 } }])).toBe('20040 km')

    // Nothing to press: no pointer cursor, no selection.
    const still = mounted({ selectable: false })
      .findComponent({ name: 'Bar' })
      .props('options') as {
      onClick: (e: unknown, els: unknown, chart: unknown) => void
      onHover: (e: unknown, els: unknown[]) => void
    }
    const canvas = document.createElement('canvas')
    still.onHover({ native: { target: canvas } }, [{}])
    expect(canvas.style.cursor).toBe('')
    still.onClick({}, [], chart([{ index: 0, datasetIndex: 0 }]))
    expect(w.findComponent(ChartFrame).emitted('select')).toHaveLength(1)
  })

  it('draws a band filled towards its lower bound, and a dashed reference', () => {
    const w = mounted({
      kind: 'line',
      series: [
        { label: 'First quartile', data: [73, null], color: '#5b8fc4' },
        { label: 'Third quartile', data: [73.75, null], color: '#5b8fc4', band: true },
        { label: 'Reference', data: [75, 75], color: '#666666', dashed: true },
      ],
    })
    const datasets = (
      w.findComponent({ name: 'Line' }).props('data') as {
        datasets: {
          fill: string | false
          borderDash?: number[]
          backgroundColor: string
          borderColor: string
        }[]
      }
    ).datasets
    expect(datasets[0]?.fill).toBe(false)
    expect(datasets[1]?.fill).toBe('-1')
    // The band fills in the band color, its bounds thin edges in their own.
    expect(datasets[1]?.backgroundColor).toBe(
      vuetify.theme.themes.value.light?.colors['chart-band'],
    )
    expect(datasets[1]?.borderColor).toBe('#5b8fc4')
    expect(datasets[2]?.borderDash).toEqual([6, 4])
  })

  it('shows each table series in its own format, and the y of a point', () => {
    const w = mounted({
      kind: 'line',
      labels: ['1 Sept'],
      series: [{ label: 'Median', data: [{ x: 1, y: 75 }], color: '#1d5b8f' }],
      tableSeries: [
        { label: 'Median', data: [75], color: '#1d5b8f' },
        { label: 'Estimates', data: [4], color: '#666666', format: () => 'four' },
      ],
    })
    const rows = w.findAll('tbody tr').map((r) => r.findAll('th, td').map((c) => c.text()))
    expect(rows).toEqual([['1 Sept', '75 km', 'four']])
    // The tooltip reads the value of a point given as {x, y}.
    const options = w.findComponent({ name: 'Line' }).props('options') as {
      plugins: {
        tooltip: {
          callbacks: { label: (c: { dataset: { label?: string }; raw: unknown }) => string }
        }
      }
    }
    expect(
      options.plugins.tooltip.callbacks.label({
        dataset: { label: 'Median' },
        raw: { x: 1, y: 75 },
      }),
    ).toBe('Median: 75 km')
  })
})
