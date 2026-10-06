import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Stats } from '@/entities/stats'
import { ApiError } from '@/shared/api'
import { StatsTiles } from '@/widgets/stats-tiles'
import stats from '@fixtures/stats.json'
import { heard, mountWith, seen, testRouter } from '@test/utils'

const { fetchStats } = vi.hoisted(() => ({ fetchStats: vi.fn() }))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats,
}))

beforeEach(() => {
  fetchStats.mockReset()
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()
const props = { vehicle: 'v1', from: 'a', to: 'b', bucket: 'day' }

async function tiles(p: Record<string, unknown> = {}) {
  const { wrapper } = mountWith(StatsTiles, { props: { ...props, ...p } })
  await flushPromises()
  return wrapper
}

// tile is the label, the value and the notes of a tile.
function tile(w: Awaited<ReturnType<typeof tiles>>, key: string) {
  const t = w.find(`.tile.${key}`)
  return {
    label: t.find('dt').text(),
    value: seen(t.find('.value').element),
    notes: t.findAll('.note').map((n) => plain(n.text())),
  }
}

describe('StatsTiles', () => {
  it('reads the period with its split, and tells each total with what it lacks', async () => {
    fetchStats.mockResolvedValue(stats)
    const w = await tiles()
    expect(fetchStats).toHaveBeenCalledWith('v1', { from: 'a', to: 'b', tz: 'UTC', bucket: 'day' })
    // A description list of div groups only.
    expect(w.findAll('dl > *').every((c) => c.element.tagName === 'DIV')).toBe(true)
    expect(tile(w, 'distance')).toEqual({ label: 'Distance', value: '73 km', notes: [] })
    expect(tile(w, 'trips')).toEqual({
      label: 'Trips',
      value: '3',
      notes: ['including 1 reconstructed'],
    })
    // A range is its two bounds, never a midpoint.
    expect(tile(w, 'driving-time').value).toMatch(
      /^At least 1 hr, 21 mins? At most 1 hr, 35 mins?$/,
    )
    expect(tile(w, 'driving-time').notes).toEqual(['1 reconstructed trip, without a duration'])
    expect(tile(w, 'consumption').value).toBe('18.8 kWh/100 km')
    expect(tile(w, 'energy')).toEqual({
      label: 'Energy charged',
      value: '44 kWh',
      notes: [
        'From the state of charge',
        'From the charging power: 25 kWh',
        'no power reading for 1 charge',
      ],
    })
    expect(tile(w, 'charges').notes).toEqual(['1 AC, 0 DC', '1 of unknown type'])
    expect(tile(w, 'parked').value).toBe('0% per day')
    expect(tile(w, 'parked').notes).toContain('unknown for 3 intervals')
  })

  it('says unknown, never 0, for an average over nothing', async () => {
    const s = structuredClone(stats) as Stats
    s.totals.trips.consumption_kwh_per_100km = null
    s.totals.trips.distance_unknown = 2
    s.totals.parked.soc_loss_pct_per_day = null
    s.totals.charges.energy_soc_unknown = 1
    fetchStats.mockResolvedValue(s)
    const w = await tiles()
    expect(tile(w, 'consumption').value).toBe('Unknown')
    expect(tile(w, 'distance').notes).toEqual(['unknown for 2 trips'])
    expect(tile(w, 'parked').value).toBe('Unknown')
    expect(tile(w, 'energy').notes).toContain('energy unknown for 1 charge')
  })

  it('tells the cost of the charges, a range read as words, with the entered and unknown', async () => {
    const s = structuredClone(stats) as Stats
    s.totals.charges.cost_unknown = 1
    fetchStats.mockResolvedValue(s)
    const w = await tiles()
    expect(tile(w, 'cost')).toEqual({
      label: 'Cost of charges',
      value: '€13.74 – €14.29',
      notes: [
        'Estimated from the tariffs, or entered; a range when the times of the charges are not known',
        'unknown for 1 charge',
        '1 cost entered',
      ],
    })
    expect(heard(w.find('.tile.cost .value').element)).toBe('from €13.74 to €14.29')
  })

  // An orphaned cost was entered for a charge detected again, which most often has its
  // tariff's cost: added to the total, it would count that charge twice.
  it('tells the costs without a charge apart, never in the total, with a way to them', async () => {
    fetchStats.mockResolvedValue(stats)
    const router = testRouter()
    const { wrapper } = mountWith(StatsTiles, { router, props })
    await flushPromises()
    expect(plain(wrapper.find('.orphans').text())).toBe(
      '1 cost without a charge: €12.40, not in the total. See the settings',
    )
    expect(wrapper.find('dl .orphans').exists()).toBe(false)
    expect(wrapper.find('.orphans a').attributes('href')).toBe('/settings')
    expect(wrapper.find('.orphans a').classes()).toContain('text-primary')
  })

  it('says unknown when every cost is, and zero over no charge', async () => {
    const s = structuredClone(stats) as Stats
    s.totals.charges.cost_unknown = s.totals.charges.count
    s.totals.charges.cost_entered = 0
    fetchStats.mockResolvedValue(s)
    expect(tile(await tiles(), 'cost').value).toBe('Unknown')
    s.totals.charges = { ...s.totals.charges, count: 0, cost_unknown: 0 }
    s.totals.charges.cost = { min_minor: 0, max_minor: 0 }
    expect(tile(await tiles(), 'cost').value).toBe('€0.00')
  })

  it('shows no cost without a currency, nor the costs without a charge', async () => {
    fetchStats.mockResolvedValue({ ...stats, currency: null })
    const w = await tiles()
    expect(w.find('.tile.cost').exists()).toBe(false)
    expect(w.find('.orphans').exists()).toBe(false)
  })

  it.each([
    [new ApiError(400, 'invalid_parameter', ''), 'too many intervals'],
    [new ApiError(404, 'not_found', ''), 'vehicle'],
    [new ApiError(500, 'internal', ''), 'could not be loaded'],
  ])('tells a failure: %s', async (err, text) => {
    fetchStats.mockRejectedValue(err)
    const w = await tiles()
    expect(w.find('.v-alert').text()).toContain(text)
    expect(w.find('dl').exists()).toBe(false)
  })
})
