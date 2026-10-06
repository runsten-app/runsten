import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Stats } from '@/entities/stats'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import { PeriodTotals } from '@/widgets/period-totals'
import empty from '@fixtures/stats-empty.json'
import totals from '@fixtures/stats-totals.json'
import { heard, mountWith, seen } from '@test/utils'

const { fetchStats } = vi.hoisted(() => ({ fetchStats: vi.fn() }))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats,
}))

beforeEach(() => {
  fetchStats.mockReset()
  i18n.global.locale.value = 'en'
})

const vehicle = 'v1'
const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function line(props: Record<string, unknown>) {
  const { wrapper } = mountWith(PeriodTotals, { props: { vehicle, ...props } })
  await flushPromises()
  return wrapper.find('.period-totals')
}

// A copy of the totals with some changes.
function withTotals(change: (t: Stats['totals']) => void) {
  const s = structuredClone(totals) as Stats
  change(s.totals)
  return s
}

describe('PeriodTotals', () => {
  it('sums the trips: count, reconstructed, distance, estimated consumption', async () => {
    fetchStats.mockResolvedValue(totals)
    const p = await line({ kind: 'trips' })
    expect(plain(p.text())).toBe(
      '3 trips in all · including 1 reconstructed · 73 km · 18.8 kWh/100 km (estimated)',
    )
    expect(fetchStats).toHaveBeenCalledWith(vehicle, {
      from: undefined,
      to: undefined,
      tz: 'UTC',
      bucket: undefined,
    })
  })

  it('sums the charges: energy from the state of charge, by type', async () => {
    fetchStats.mockResolvedValue(totals)
    const p = await line({ kind: 'charges' })
    expect(seen(p.element)).toBe(
      '2 charges in all · including 1 reconstructed · 44 kWh (from the state of charge) · 1 AC, 0 DC · 1 of unknown type · €5.24 – €5.79 · cost unknown for 1 charge',
    )
  })

  // A range is read as words, not "5.24 dash 5.79".
  it('sums the known costs, read as a range by a screen reader', async () => {
    fetchStats.mockResolvedValue(totals)
    const p = await line({ kind: 'charges' })
    expect(heard(p.element)).toMatch(/ · from €5\.24 to €5\.79 · cost unknown for 1 charge$/)
  })

  it('tells the entered costs, and a single amount as it is', async () => {
    fetchStats.mockResolvedValue(
      withTotals((t) => {
        t.charges = { ...t.charges, cost: { min_minor: 850, max_minor: 850 } }
        t.charges.cost_unknown = 0
        t.charges.cost_entered = 2
      }),
    )
    const p = await line({ kind: 'charges' })
    expect(seen(p.element)).toMatch(/ · €8\.50 · 2 costs entered$/)
    expect(heard(p.element)).toMatch(/ · €8\.50 · 2 costs entered$/)
  })

  it('shows no cost without a currency, nor on the trips', async () => {
    fetchStats.mockResolvedValue({ ...totals, currency: null })
    expect(seen((await line({ kind: 'charges' })).element)).toMatch(/1 of unknown type$/)
    fetchStats.mockResolvedValue(totals)
    expect(seen((await line({ kind: 'trips' })).element)).not.toContain('€')
  })

  it('counts the events started in a filtered period', async () => {
    fetchStats.mockResolvedValue(totals)
    const p = await line({ kind: 'trips', from: '2026-09-28T00:00:00.000Z' })
    expect(p.text()).toMatch(/^3 trips started in the period · /)
    expect(fetchStats).toHaveBeenCalledWith(
      vehicle,
      expect.objectContaining({ from: '2026-09-28T00:00:00.000Z' }),
    )
  })

  it('tells what is unknown, never a zero in its place', async () => {
    fetchStats.mockResolvedValue(
      withTotals((t) => {
        t.trips = {
          ...t.trips,
          count: 1,
          reconstructed: 0,
          distance_unknown: 2,
          consumption_kwh_per_100km: null,
        }
        t.charges = { ...t.charges, count: 1, reconstructed: 0, energy_soc_unknown: 1 }
        t.charges.by_type = {
          ...t.charges.by_type,
          ac: { ...t.charges.by_type.ac, count: 0 },
          unknown: { ...t.charges.by_type.unknown, count: 0 },
        }
      }),
    )
    expect(plain((await line({ kind: 'trips' })).text())).toBe(
      '1 trip in all · 73 km · distance unknown for 2 trips · consumption unknown',
    )
    // No sum of nothing: every cost is unknown.
    expect(seen((await line({ kind: 'charges' })).element)).toBe(
      '1 charge in all · 44 kWh (from the state of charge) · energy unknown for 1 charge · cost unknown for 1 charge',
    )
  })

  it('says nothing without an event: the list tells it is empty', async () => {
    fetchStats.mockResolvedValue(empty)
    expect((await line({ kind: 'trips' })).exists()).toBe(false)
    expect((await line({ kind: 'charges' })).exists()).toBe(false)
  })

  it('tells when the totals cannot be read', async () => {
    fetchStats.mockRejectedValue(new ApiError(500, 'internal', 'boom'))
    expect((await line({ kind: 'trips' })).text()).toBe(
      'The totals of the period could not be loaded.',
    )
  })

  it('speaks French and Swedish', async () => {
    fetchStats.mockResolvedValue(totals)
    i18n.global.locale.value = 'fr'
    const fr = await line({ kind: 'charges', to: 'x' })
    expect(seen(fr.element)).toMatch(
      /^2 charges commencées dans la période · dont 1 reconstituée · 44 kWh \(d'après l'état de charge\)/,
    )
    expect(seen(fr.element)).toMatch(/ · 5,24–5,79 € · coût inconnu pour 1 charge$/)
    expect(heard(fr.element)).toMatch(/ · de 5,24 € à 5,79 € · coût inconnu pour 1 charge$/)
    i18n.global.locale.value = 'sv'
    expect(plain((await line({ kind: 'trips' })).text())).toMatch(
      /^3 resor totalt · varav 1 rekonstruerad · 73 km/,
    )
  })
})
