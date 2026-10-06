import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/shared/api'
import { ChargeList } from '@/widgets/charge-list'
import charges from '@fixtures/charges.json'
import { heard, mountWith, seen, testRouter } from '@test/utils'

const { fetchCharges } = vi.hoisted(() => ({ fetchCharges: vi.fn() }))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchCharges,
}))

beforeEach(() => {
  fetchCharges.mockReset()
})

const vehicle = 'v1'
const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mounted(props: Record<string, unknown> = {}) {
  const router = testRouter()
  await router.push(`/vehicles/${vehicle}/charges`)
  const { wrapper } = mountWith(ChargeList, { router, props: { vehicle, ...props } })
  await flushPromises()
  return wrapper
}

describe('ChargeList', () => {
  it('shows the charges, the reconstructed one without a duration', async () => {
    fetchCharges.mockResolvedValue(charges)
    const w = await mounted()
    const [seen, reconstructed] = w.findAll('a.charge-summary')
    expect(seen?.find('.event-when').text()).toBe('18:20–18:30 → 21:55–21:56')
    expect(plain(seen?.find('.figures').text())).toBe(
      'AC · 47% → 80% · 3 hrs, 25 mins – 3 hrs, 36 mins',
    )
    expect(seen?.attributes('href')).toBe(
      `/vehicles/${vehicle}/charges/2026-09-28T18:30:00.000000Z`,
    )
    expect(reconstructed?.find('.event-when').text()).toBe('Happened between 01:00 and 04:00')
    expect(plain(reconstructed?.find('.figures').text())).toBe('40% → 62%')
    expect(reconstructed?.find('.reconstructed').exists()).toBe(true)
    expect(w.find('.end').text()).toBe('No more charges.')
  })

  // Short: an unknown cost is told in the details, not on every line.
  it('shows the cost of each charge when it is known, a range read as words', async () => {
    const [c] = charges.items
    fetchCharges.mockResolvedValue({
      items: [
        c,
        { ...c, id: 'free', cost: { ...c?.cost, min_minor: 0, max_minor: 0 } },
        charges.items[1],
      ],
      next_cursor: null,
    })
    const w = await mounted()
    const costs = w.findAll('a.charge-summary').map((a) => a.find('.cost'))
    expect(costs.map((c) => (c.exists() ? seen(c.element) : null))).toEqual([
      '€5.24 – €5.79',
      'Free',
      null,
    ])
    expect(heard(costs[0]?.element ?? document.body)).toBe('from €5.24 to €5.79')
  })

  it('says when the state of charge is unknown', async () => {
    const [c] = charges.items
    fetchCharges.mockResolvedValue({
      items: [{ ...c, type: null, start_soc_pct: null, end_soc_pct: null }],
      next_cursor: null,
    })
    const w = await mounted()
    expect(plain(w.find('.figures').text())).toBe(
      'State of charge unknown · 3 hrs, 25 mins – 3 hrs, 36 mins',
    )
  })

  it('tells an empty period and a failure', async () => {
    fetchCharges.mockResolvedValue({ items: [], next_cursor: null })
    expect((await mounted({ to: '2026-09-01T00:00:00.000Z' })).find('.empty').text()).toBe(
      'No charge in this period.',
    )
    fetchCharges.mockRejectedValue(new ApiError(500, 'internal', ''))
    expect((await mounted()).find('.v-alert').text()).toBe(
      'The charges could not be loaded. Try again in a moment.',
    )
  })

  // A list's items are listitems: its rows are links, the divider goes inside an item.
  it('holds listitems only, each with its link', async () => {
    fetchCharges.mockResolvedValue(charges)
    const w = await mounted()
    for (const list of w.findAll('[role="list"]')) {
      const items = list.element.children
      expect(items.length).toBeGreaterThan(0)
      for (const item of items) {
        expect(item.getAttribute('role')).toBe('listitem')
        expect(item.querySelector('a.charge-summary')).not.toBeNull()
      }
    }
  })
})
