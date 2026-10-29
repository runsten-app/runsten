import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Point, Series } from '@/shared/ui/chart'
import { TyreChart } from '@/widgets/tyre-chart'
import tyres from '@fixtures/tyres.json'
import { mountWith } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const mocks = vi.hoisted(() => ({ fetchTyres: vi.fn() }))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  ...mocks,
}))

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T23:10:00Z'))
  mocks.fetchTyres.mockReset()
  mocks.fetchTyres.mockResolvedValue(tyres)
})
afterEach(() => {
  vi.useRealTimers()
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mounted() {
  const { wrapper } = mountWith(TyreChart, { props: { vehicle: 'v1' } })
  await flushPromises()
  return wrapper
}

describe('TyreChart', () => {
  it('asks the last 30 days, up to the next hour', async () => {
    await mounted()
    expect(mocks.fetchTyres).toHaveBeenCalledWith('v1', {
      from: '2026-08-30T00:00:00.000Z',
      to: '2026-09-29T00:00:00.000Z',
    })
  })

  it('draws each axle, its tyres in bar and the recommended pressure dashed', async () => {
    const w = await mounted()
    const [front, rear] = w.findAllComponents({ name: 'ChartFrame' })
    expect(front?.props('title')).toBe('Front tyres')
    const [left, right, recommended] = rear?.props('series') as Series[]
    expect((left?.data as Point[]).map((p) => p.y)).toEqual([2.7, 2.7, null, 2.48, 2.48])
    // The left one under the right one, which shows within it when they are equal.
    expect(left?.wide).toBe(true)
    expect(right?.wide).toBeUndefined()
    expect(rear?.props('hint')).toContain('The dashed line')
    expect(recommended).toMatchObject({ label: 'Recommended', dashed: true })
    expect((recommended?.data as Point[])[0]?.y).toBe(2.9)
    // One day in the table: the lowest and the highest of each tyre.
    expect(rear?.props('labels')).toHaveLength(1)
    const table = rear?.props('tableSeries') as Series[]
    expect(table.map((s) => s.data)).toEqual([[2.48], [2.7], [null], [null]])
    expect(plain(rear?.props('summary'))).toBe(
      'From 2.7 bar to 2.48 bar on the left, from Unknown to Unknown on the right.',
    )
  })

  it('has no recommended pressure where the vehicle gives none', async () => {
    mocks.fetchTyres.mockResolvedValue({
      ...tyres,
      runs: tyres.runs.map((run) => ({
        readings: run.readings.map((r) => ({
          ...r,
          recommended_front_kpa: null,
          recommended_rear_kpa: null,
        })),
      })),
    })
    const w = await mounted()
    for (const chart of w.findAllComponents({ name: 'ChartFrame' })) {
      expect((chart.props('series') as Series[]).map((s) => s.label)).toEqual(['Left', 'Right'])
      expect(chart.props('hint')).not.toContain('dashed')
    }
  })

  it('asks another window when chosen, split by month over a year', async () => {
    const w = await mounted()
    const buttons = w.findAll('[role="group"] button')
    expect(buttons.map((b) => b.text())).toEqual(['7 days', '30 days', '12 months'])
    await buttons[2]?.trigger('click')
    await flushPromises()
    expect(mocks.fetchTyres).toHaveBeenLastCalledWith('v1', {
      from: '2025-09-29T00:00:00.000Z',
      to: '2026-09-29T00:00:00.000Z',
    })
    expect(w.find('[role="group"] button[aria-pressed="true"]').text()).toBe('12 months')
    const [front] = w.findAllComponents({ name: 'ChartFrame' })
    expect(front?.props('labels')).toEqual(['September 2026'])
  })

  it('says when no pressure was read', async () => {
    mocks.fetchTyres.mockResolvedValue({ ...tyres, runs: [] })
    const w = await mounted()
    const [front] = w.findAllComponents({ name: 'ChartFrame' })
    expect(front?.props('empty')).toBe('No pressure was read in this period.')
  })
})
