import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/shared/api'
import { ChargeDetails } from '@/widgets/charge-details'
import entered from '@fixtures/charge-cost.json'
import charge from '@fixtures/charge.json'
import charges from '@fixtures/charges.json'
import settings from '@fixtures/settings.json'
import unset from '@fixtures/settings-unset.json'
import { sessionKeys } from '@/entities/session'
import { i18n } from '@/shared/i18n'
import session from '@fixtures/session.json'
import { heard, mountWith, seen, testQueryClient } from '@test/utils'

// The public build's: no page lifts the limits.
vi.mock('@/entities/session/extensions', () => ({ limitsPage: null }))

const { fetchCharge, fetchSettings } = vi.hoisted(() => ({
  fetchCharge: vi.fn(),
  fetchSettings: vi.fn(),
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  fetchCharge,
}))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  fetchSettings,
}))

beforeEach(() => {
  fetchCharge.mockReset()
  fetchSettings.mockReset()
  fetchSettings.mockResolvedValue(settings)
})

const plain = (s: string) => s.replace(/\s+/g, ' ').trim()

async function mounted(body: unknown, unavailable: string[] = []) {
  if (body instanceof Error) fetchCharge.mockRejectedValue(body)
  else fetchCharge.mockResolvedValue(body)
  const queryClient = testQueryClient()
  queryClient.setQueryData(sessionKeys.current(), {
    ...session,
    limits: unavailable.length ? { history_from: null, unavailable, unread_vehicles: [] } : null,
  })
  const { wrapper } = mountWith(ChargeDetails, { queryClient, props: { vehicle: 'v1', id: 'x' } })
  await flushPromises()
  return wrapper
}

type Wrapper = Awaited<ReturnType<typeof mounted>>

function facts(w: Wrapper, card: string) {
  return Object.fromEntries(
    w.findAll(`.${card} .fact`).map((f) => [f.find('dt').text(), seen(f.find('.value').element)]),
  )
}

const costNotes = (w: Wrapper) => w.findAll('.cost-note').map((n) => plain(n.text()))

describe('ChargeDetails', () => {
  it('shows the bounds, the duration, the type and the target', async () => {
    const w = await mounted(charge)
    expect(w.find('h1').text()).toBe('Charge')
    expect(facts(w, 'when')).toEqual({
      Start: '18:20–18:30',
      End: '21:55–21:56',
      Duration: '3 hrs, 25 mins – 3 hrs, 36 mins',
    })
    expect(facts(w, 'figures')).toEqual({
      Type: 'AC',
      'State of charge': '47% → 80%',
      Target: '80%',
    })
    expect(facts(w, 'position')).toEqual({ Coordinates: '45.76400, 4.83570' })
    expect(w.find('.position a').attributes('href')).toContain('openstreetmap.org')
  })

  it('gives the address of the position, where the geocoder found one', async () => {
    const w = await mounted({ ...charge, place: null, address: 'Rue de la République, Lyon' })
    expect(facts(w, 'position')).toEqual({
      Address: 'Rue de la République, Lyon',
      Coordinates: '45.76400, 4.83570',
    })
  })

  it('shows both energy estimates, and says they are estimates', async () => {
    const w = await mounted(charge)
    expect(facts(w, 'energies')).toEqual({
      'Energy (from state of charge)': '26.4 kWh',
      'Energy (from charging power)': '25.3 kWh',
    })
    expect(w.find('.energies .note').text()).toBe(
      'Two separate estimates, neither a measurement: the vehicle has no energy meter to read.',
    )
  })

  it('tells a reconstructed charge, both energies still shown', async () => {
    const w = await mounted(charges.items[1])
    expect(w.find('.reconstructed-note .event-when').text()).toBe(
      'Happened between 01:00 and 04:00',
    )
    expect(w.find('.when').exists()).toBe(false)
    expect(facts(w, 'energies')).toEqual({
      'Energy (from state of charge)': '17.6 kWh',
      'Energy (from charging power)': 'Unknown',
    })
    expect(facts(w, 'figures')).toEqual({
      Type: 'Unknown',
      'State of charge': '40% → 62%',
      Target: 'Unknown',
    })
    expect(w.find('.position a').exists()).toBe(false)
  })

  it('tells an unknown charge', async () => {
    const w = await mounted(new ApiError(404, 'not_found', ''))
    expect(w.find('.v-alert').text()).toBe('This charge does not exist.')
  })

  it('tells another failure', async () => {
    const w = await mounted(new ApiError(0, 'network', ''))
    expect(w.find('.v-alert').text()).toBe('The charge could not be loaded. Try again in a moment.')
  })

  // Each card is a section of the page, under its <h1>.
  it('titles its cards with headings', async () => {
    const w = await mounted(charge)
    expect(w.findAll('.v-card-title').map((h) => [h.element.tagName, h.text()])).toEqual([
      ['H2', 'When'],
      ['H2', 'Figures'],
      ['H2', 'Energy'],
      ['H2', 'Cost'],
      ['H2', 'Position'],
    ])
  })

  it("estimates a cost from its place's tariff: a range, read as words, and why", async () => {
    const w = await mounted(charge)
    expect(facts(w, 'cost')).toEqual({ Place: 'Maison de Tante Agathe', Cost: '€5.24 – €5.79' })
    expect(heard(w.findAll('.cost .fact .value')[1]?.element ?? document.body)).toBe(
      'from €5.24 to €5.79',
    )
    expect(costNotes(w)).toEqual([
      'Estimated from the tariff of Maison de Tante Agathe',
      '30 kWh billed (estimated), assuming an efficiency of 88%',
      'The exact time of the charge is not known: its cost lies within this range.',
    ])
    const link = w.find('.cost .place-link')
    expect(link.text()).toBe('Open the place and its tariff')
    // In the theme's color, not the browser's blue.
    expect(link.classes()).toContain('text-primary')
    expect(link.attributes('href')).toBe('/settings/places/5e1f0000-0000-4000-8000-000000000001')
    expect(w.find('.create-place').exists()).toBe(false)
    expect(w.find('.charge-cost-editor .toggle').text()).toBe('Enter the cost')
    expect(w.find('.delete-cost').exists()).toBe(false)
  })

  it('shows an entered cost, its energy and its note, to change or delete', async () => {
    const w = await mounted(entered)
    expect(facts(w, 'cost')).toEqual({ Place: 'Outside any place', Cost: '€8.50' })
    expect(costNotes(w)).toEqual([
      'Entered',
      '20.1 kWh billed',
      'Note: Borne du parking, carte Chargemap',
    ])
    expect(w.find('.charge-cost-editor .toggle').text()).toBe('Change the entered cost')
    expect(w.find('.delete-cost').text()).toBe('Delete the entered cost')
    // Without a position, no place can be made from it.
    expect(w.find('.create-place').exists()).toBe(false)
  })

  it('says an entered 0 is free, and an entered cost without energy nor note', async () => {
    const w = await mounted({
      ...entered,
      cost: { ...entered.cost, min_minor: 0, max_minor: 0, energy_kwh: null, note: null },
    })
    expect(facts(w, 'cost').Cost).toBe('Free')
    expect(costNotes(w)).toEqual(['Entered'])
  })

  it('offers to create a place at the position of a charge outside any', async () => {
    const w = await mounted({ ...charge, place: null, cost: null })
    expect(facts(w, 'cost')).toEqual({ Place: 'Outside any place', Cost: 'Unknown' })
    expect(costNotes(w)).toEqual(['Neither a tariff nor an entered cost gives it.'])
    const create = w.find('.create-place')
    expect(create.text()).toBe('Create a place here')
    expect(create.attributes('href')).toBe('/settings/places/new?lat=45.76400&lon=4.83570')
  })

  it('tells a charge at a place that its tariff gives no price, and leads to it', async () => {
    const w = await mounted({ ...charge, cost: null })
    expect(facts(w, 'cost')).toEqual({ Place: charge.place?.name, Cost: 'Unknown' })
    expect(costNotes(w)).toEqual([
      `The tariff of ${charge.place?.name} gives no price on the day of this charge: it started before the tariff's first price.`,
    ])
    expect(w.find('.charge-cost-editor').exists()).toBe(true)
  })

  it('does not blame the tariff of a charge whose energy is unknown', async () => {
    const w = await mounted({ ...charge, energy_soc_kwh: null, cost: null })
    expect(costNotes(w)).toEqual(['Neither a tariff nor an entered cost gives it.'])
  })

  it('tells an unknown cost without a place nor a position', async () => {
    const w = await mounted(charges.items[1])
    expect(facts(w, 'cost')).toEqual({ Place: 'Outside any place', Cost: 'Unknown' })
    expect(w.find('.create-place').exists()).toBe(false)
    expect(w.find('.charge-cost-editor').exists()).toBe(true)
  })

  it('leads to the settings without a currency, and offers no entry', async () => {
    fetchSettings.mockResolvedValue(unset)
    const w = await mounted({ ...charge, cost: null })
    expect(facts(w, 'cost').Cost).toBe('Unknown')
    expect(costNotes(w)).toEqual(['No currency chosen yet: costs cannot be computed.'])
    const choose = w.find('.choose-currency')
    expect(choose.text()).toBe('Choose a currency')
    expect(choose.attributes('href')).toBe('/settings')
    expect(w.find('.charge-cost-editor').exists()).toBe(false)
  })

  it('says nothing more of a cost while the settings are unknown', async () => {
    fetchSettings.mockRejectedValue(new ApiError(500, 'internal', ''))
    const w = await mounted({ ...charge, cost: null })
    expect(facts(w, 'cost').Cost).toBe('Unknown')
    expect(costNotes(w)).toEqual([])
    expect(w.find('.charge-cost-editor').exists()).toBe(false)
  })

  it("says the costs are part of another offer, where the account's leaves them out", async () => {
    const w = await mounted({ ...charge, cost: null }, ['costs'])
    expect(facts(w, 'cost')).toEqual({ Place: 'Maison de Tante Agathe' })
    expect(w.find('.cost .feature-unavailable').text()).toContain(i18n.global.t('limits.costs'))
    expect(w.find('.charge-cost-editor').exists()).toBe(false)
    expect(w.find('.choose-currency').exists()).toBe(false)
  })
})
