import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Series as ApiSeries } from '@/entities/series'
import type { Series } from '@/shared/ui/chart'
import { ChargeCurve } from '@/widgets/charge-curve'
import charge from '@fixtures/charge.json'
import charges from '@fixtures/charges.json'
import series from '@fixtures/series.json'
import { mountWith } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const mocks = vi.hoisted(() => ({ fetchSeries: vi.fn(), fetchCharge: vi.fn() }))
vi.mock('@/entities/series', async (original) => ({
  ...(await original<typeof import('@/entities/series')>()),
  fetchSeries: mocks.fetchSeries,
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchCharge: mocks.fetchCharge,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchCharge.mockResolvedValue(charge)
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

// charging is a run of readings a minute apart from 18:30, the power at 7.4 kW and the
// state of charge rising a point a minute.
function charging(n: number): ApiSeries {
  const at = (i: number) => new Date(Date.parse('2026-09-28T18:30:00Z') + i * 60_000)
  const readings = Array.from({ length: n }, (_, i) => ({
    at: at(i).toISOString().replace('.000', ''),
    soc_pct: 47 + i,
    power_w: 7400,
    range_km: 190 + i * 4,
  }))
  return { from: charge.start.after, to: charge.end.before, runs: n ? [{ readings }] : [] }
}

async function mounted(id = charge.id) {
  const { wrapper } = mountWith(ChargeCurve, { props: { vehicle: 'v1', id } })
  await flushPromises()
  return wrapper
}

describe('ChargeCurve', () => {
  it("asks the charge's readings, between its bounds", async () => {
    mocks.fetchSeries.mockResolvedValue(charging(6))
    await mounted()
    expect(mocks.fetchSeries).toHaveBeenCalledWith('v1', {
      from: '2026-09-28T18:20:00Z',
      to: '2026-09-28T21:56:00.000Z',
    })
  })

  it('draws the power and, on its own axis, the state of charge', async () => {
    mocks.fetchSeries.mockResolvedValue(charging(6))
    const w = await mounted()
    const frame = w.findComponent({ name: 'ChartFrame' })
    const [power, soc] = frame.props('series') as Series[]
    expect(power?.data).toHaveLength(6)
    expect((power?.data[0] as { y: number }).y).toBe(7.4)
    expect(soc?.right).toBe(true)
    expect(frame.props('rightUnit')).toBe('%')
    expect(plain(w.find('canvas').attributes('aria-label'))).toBe(
      'Up to 7.4 kW; state of charge from 47% to 52%.',
    )
    expect(w.findAll('tbody tr')).toHaveLength(6)
    expect(
      w
        .findAll('tbody tr')[0]
        ?.findAll('td')
        .map((c) => plain(c.text())),
    ).toEqual(['7.4 kW', '47%'])
  })

  it('draws the readings of the fixtures, the line broken between their runs', async () => {
    mocks.fetchSeries.mockResolvedValue(series)
    const w = await mounted()
    const [power] = w.findComponent({ name: 'ChartFrame' }).props('series') as Series[]
    expect((power?.data as { y: number | null }[]).map((p) => p.y)).toEqual([
      0,
      null,
      0,
      0,
      7.4,
      7.35,
    ])
  })

  it('says how few readings there are rather than draw them', async () => {
    // Read every hour: the power at four readings only.
    mocks.fetchSeries.mockResolvedValue(charging(4))
    const w = await mounted()
    expect(plain(w.find('.empty').text())).toBe(
      '4 readings of the power during this charge: too few to draw its curve.',
    )
    expect(w.find('canvas').exists()).toBe(false)
  })

  it('has none for a reconstructed charge, never seen', async () => {
    const reconstructed = charges.items.find((c) => c.reconstructed)
    mocks.fetchCharge.mockResolvedValue(reconstructed)
    const w = await mounted(reconstructed?.id)
    expect(mocks.fetchSeries).not.toHaveBeenCalled()
    expect(w.find('.charge-curve').exists()).toBe(false)
  })
})
