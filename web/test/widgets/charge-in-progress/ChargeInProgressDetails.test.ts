import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { sessionKeys } from '@/entities/session'
import { ChargeInProgressDetails } from '@/widgets/charge-in-progress'
import type { ChargeInProgress } from '@/entities/charge'
import charging from '@fixtures/charging.json'
import session from '@fixtures/session.json'
import settings from '@fixtures/settings.json'
import unset from '@fixtures/settings-unset.json'
import { mountWith, seen, testQueryClient } from '@test/utils'

// The public build's: no page lifts the limits.
vi.mock('@/entities/session/extensions', () => ({ limitsPage: null }))

const { fetchSettings } = vi.hoisted(() => ({ fetchSettings: vi.fn() }))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  fetchSettings,
}))

const open = charging.charge as ChargeInProgress

beforeEach(() => {
  fetchSettings.mockReset()
  fetchSettings.mockResolvedValue(settings)
})
afterEach(() => {
  vi.useRealTimers()
})

const plain = (s: string) => s.replace(/\s+/g, ' ').trim()

// mounted shows the charge in progress at now; its latest reading is at 23:12.
async function mounted(now: string, charge: ChargeInProgress = open, unavailable: string[] = []) {
  vi.useFakeTimers({ now: Date.parse(now), toFake: ['Date'] })
  const queryClient = testQueryClient()
  queryClient.setQueryData(sessionKeys.current(), {
    ...session,
    limits: unavailable.length ? { history_from: null, unavailable, unread_vehicles: [] } : null,
  })
  const { wrapper } = mountWith(ChargeInProgressDetails, { queryClient, props: { charge } })
  await flushPromises()
  return wrapper
}

type Wrapper = Awaited<ReturnType<typeof mounted>>

function facts(w: Wrapper, card: string) {
  return Object.fromEntries(
    w.findAll(`.${card} .fact`).map((f) => [f.find('dt').text(), seen(f.find('.value').element)]),
  )
}

describe('ChargeInProgressDetails', () => {
  it('shows what the charge added so far, its power and its cost so far', async () => {
    const w = await mounted('2026-09-28T23:13:00Z')
    expect(w.find('h1').text()).toBe('Charge in progress')
    expect(w.find('.stopped').exists()).toBe(false)
    // Read once more after its latest reading charging: paused.
    expect(w.find('.state').text()).toBe('Paused')
    expect(plain(facts(w, 'when')['Latest reading'] ?? '')).toBe('23:12 (1 min ago)')
    expect(facts(w, 'figures')).toMatchObject({
      Type: 'AC',
      'State of charge': '40% → 52%',
      Target: '80%',
      Power: '7.4 kW',
    })
    expect(facts(w, 'energies')).toMatchObject({
      'Energy (from state of charge)': '9.6 kWh',
      'Energy (from charging power)': '9.1 kWh',
    })
    const cost = facts(w, 'cost')
    expect(cost['Place']).toContain('Maison de Tante Agathe')
    expect(cost['Cost so far']).toContain('€1.73')
    expect(w.find('.cost-note').text()).toBe(
      'Estimated from the tariff of Maison de Tante Agathe, up to the latest reading',
    )
  })

  it('says it is in progress while it charges', async () => {
    const w = await mounted('2026-09-28T23:13:00Z', { ...open, charging_at: open.read_at })
    expect(w.find('.state').text()).toBe('In progress')
  })

  // The collector stopped reading: never a charge that seems to go on.
  it('tells the readings stopped past 15 minutes', async () => {
    const w = await mounted('2026-09-28T23:40:00Z')
    expect(w.find('.state').text()).toBe('Readings stopped')
    expect(plain(w.find('.stopped').text())).toBe(
      'Nothing was read after 23:12: the charge may have ended since, unseen. It is shown as it was at its latest reading.',
    )
  })

  it('tells why the cost is unknown, and nothing of it without the costs', async () => {
    fetchSettings.mockResolvedValue(unset)
    let w = await mounted('2026-09-28T23:13:00Z', { ...open, cost: null })
    expect(facts(w, 'cost')['Cost so far']).toBe('Unknown')
    expect(w.find('.cost-note').text()).toContain('No currency chosen yet')
    vi.useRealTimers()
    w = await mounted('2026-09-28T23:13:00Z', open, ['costs'])
    expect(facts(w, 'cost')['Cost so far']).toBeUndefined()
  })
})
