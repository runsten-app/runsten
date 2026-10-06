import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { OrphanCosts } from '@/features/manage-orphan-costs'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import entered from '@fixtures/charge-cost.json'
import charges from '@fixtures/charges.json'
import orphans from '@fixtures/orphans.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchOrphanCosts: vi.fn(),
  fetchCharges: vi.fn(),
  attachOrphanCost: vi.fn(),
  deleteOrphanCost: vi.fn(),
}))
vi.mock('@/entities/charge', async (original) => ({
  ...(await original<typeof import('@/entities/charge')>()),
  ...api,
}))

const orphan = orphans.items[0]
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  i18n.global.locale.value = 'en'
  api.fetchOrphanCosts.mockResolvedValue(orphans.items)
  api.fetchCharges.mockResolvedValue({
    items: [charges.items[0], { ...entered, id: 'e' }],
    next_cursor: null,
  })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function section() {
  wrapper = mountWith(OrphanCosts, { attachTo: document.body }).wrapper
  await flushPromises()
  return wrapper
}
const w = () => {
  if (!wrapper) throw new Error('not mounted')
  return wrapper
}
const dialog = () => document.querySelector('[role="dialog"]')
const click = async (el: Element | null | undefined) => {
  ;(el as HTMLElement | null)?.click()
  await flushPromises()
}
const button = (text: string) =>
  [...document.querySelectorAll('[role="dialog"] button')].find(
    (b) => b.textContent?.trim() === text,
  )

describe('OrphanCosts', () => {
  it('lists each cost without a charge, and says where they come from', async () => {
    await section()
    expect(w().find('h2').text()).toBe('Costs without a charge')
    expect(w().find('p').text()).toMatch(
      /^A cost you entered stays even when its charge is detected again/,
    )
    expect(w().find('.orphan h3').text()).toBe('Charge of 29 Sept 2026, 12:00–12:40')
    const facts = w()
      .findAll('.orphan .fact')
      .map((f) => [f.find('dt').text(), plain(f.find('dd').text())])
    expect(facts).toEqual([
      ['Amount', '€12.40'],
      ['Billed energy', '31.5 kWh'],
      ['Note', 'Ionity, carte Chargemap'],
    ])
    // Each button names its cost: several cards have the same buttons.
    expect(w().find('.attach-orphan').attributes('aria-label')).toBe('Attach to a charge, €12.40')
    expect(w().find('.delete-orphan').attributes('aria-label')).toBe('Delete, €12.40')
  })

  it('does not show without any', async () => {
    api.fetchOrphanCosts.mockResolvedValue([])
    await section()
    expect(w().find('section').exists()).toBe(false)
  })

  it('tells a failure to read them', async () => {
    api.fetchOrphanCosts.mockRejectedValue(new ApiError(500, 'internal', ''))
    await section()
    expect(w().find('.v-alert').text()).toBe(
      'The costs without a charge could not be loaded. Try again in a moment.',
    )
  })

  it('shows a cost without a currency, energy nor note as such', async () => {
    api.fetchOrphanCosts.mockResolvedValue([
      { ...orphan, currency: null, energy_kwh: null, note: null },
    ])
    await section()
    expect(
      w()
        .findAll('.orphan dd')
        .map((d) => plain(d.text())),
    ).toEqual(['1240 minor units, no currency chosen yet', 'Not entered', 'None'])
  })

  it('attaches a cost to a charge chosen among those around its window', async () => {
    api.attachOrphanCost.mockResolvedValue(charges.items[0])
    await section()
    await w().find('.attach-orphan').trigger('click')
    await flushPromises()
    expect(dialog()?.getAttribute('aria-modal')).toBe('true')
    // Its window widened by 12 hours on each side.
    expect(api.fetchCharges).toHaveBeenCalledWith(orphan?.vehicle_id, {
      from: '2026-09-29T00:00:00.000Z',
      to: '2026-09-30T00:40:00.000Z',
    })
    const labels = [...document.querySelectorAll('[role="dialog"] .v-radio label')].map((l) =>
      plain(l.textContent ?? ''),
    )
    expect(labels).toEqual([
      '28 Sept 2026, 18:20–21:56 · AC · 26.4 kWh · €5.24 – €5.79',
      '28 Sept 2026, 01:00–04:00 · type unknown · 17.6 kWh · €8.50 entered',
    ])
    expect(document.querySelector('.confirm-attach')?.hasAttribute('disabled')).toBe(true)
    await click(document.querySelector('[role="dialog"] input[type="radio"]'))
    await click(document.querySelector('.confirm-attach'))
    expect(api.attachOrphanCost).toHaveBeenCalledWith(
      orphan?.id,
      orphan?.vehicle_id,
      charges.items[0]?.id,
    )
    expect(w().find('[role="status"]').text()).toBe('Cost attached to the charge.')
    expect(document.activeElement).toBe(w().find('h2').element)
  })

  it('explains a charge that already has an entered cost', async () => {
    api.attachOrphanCost.mockRejectedValue(new ApiError(409, 'charge_has_cost', ''))
    await section()
    await w().find('.attach-orphan').trigger('click')
    await flushPromises()
    const radios = document.querySelectorAll('[role="dialog"] input[type="radio"]')
    await click(radios[1])
    await click(document.querySelector('.confirm-attach'))
    expect(document.querySelector('[role="dialog"] .v-alert')?.textContent?.trim()).toBe(
      'This charge already has an entered cost: delete it on the charge first, or choose another one.',
    )
    // Another choice clears it; a cancel leaves the cost where it was.
    await click(radios[0])
    expect(document.querySelector('[role="dialog"] .v-alert')).toBeNull()
    await click(button('Cancel'))
    expect(w().find('[role="status"]').text()).toBe('')
    expect(document.activeElement).toBe(w().find('.attach-orphan').element)
  })

  it.each([
    [new ApiError(404, 'not_found', ''), 'This cost or this charge no longer exists.'],
    [new ApiError(500, 'internal', ''), 'The cost could not be attached. Try again later.'],
  ])('tells another refusal %o', async (error, text) => {
    api.attachOrphanCost.mockRejectedValue(error)
    await section()
    await w().find('.attach-orphan').trigger('click')
    await flushPromises()
    await click(document.querySelector('[role="dialog"] input[type="radio"]'))
    await click(document.querySelector('.confirm-attach'))
    expect(document.querySelector('[role="dialog"] .v-alert')?.textContent?.trim()).toBe(text)
  })

  it('says when no charge is around its window, or when they cannot be read', async () => {
    api.fetchCharges.mockResolvedValue({ items: [], next_cursor: null })
    await section()
    await w().find('.attach-orphan').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"] .none')?.textContent?.trim()).toBe(
      'No charge around this time: delete the cost, or keep it here.',
    )
    await click(button('Cancel'))
    wrapper?.unmount()
    api.fetchCharges.mockRejectedValue(new ApiError(500, 'internal', ''))
    await section()
    await w().find('.attach-orphan').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"] .v-alert')?.textContent?.trim()).toBe(
      'The charges could not be loaded. Try again in a moment.',
    )
  })

  it('deletes a cost once confirmed, and stays to say so when it was the last', async () => {
    api.deleteOrphanCost.mockResolvedValue(undefined)
    await section()
    await w().find('.delete-orphan').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"] p')?.textContent?.trim()).toBe(
      'The cost of €12.40 is deleted for good.',
    )
    api.fetchOrphanCosts.mockResolvedValue([])
    await click(document.querySelector('.confirm-delete'))
    expect(api.deleteOrphanCost).toHaveBeenCalledWith(orphan?.id)
    expect(w().find('.orphan').exists()).toBe(false)
    expect(w().find('.none').text()).toBe('No cost without a charge any more.')
    expect(w().find('[role="status"]').text()).toBe('Cost deleted.')
    expect(document.activeElement).toBe(w().find('h2').element)
  })

  it('tells a failed deletion in the dialog, and a cancel keeps the cost', async () => {
    api.deleteOrphanCost.mockRejectedValue(new ApiError(500, 'internal', ''))
    await section()
    await w().find('.delete-orphan').trigger('click')
    await flushPromises()
    await click(document.querySelector('.confirm-delete'))
    expect(document.querySelector('[role="dialog"] .v-alert')?.textContent?.trim()).toBe(
      'The cost could not be deleted. Try again later.',
    )
    await click(button('Cancel'))
    expect(w().find('.orphan').exists()).toBe(true)
    expect(document.activeElement).toBe(w().find('.delete-orphan').element)
  })

  it('closes a dialog with Escape, the cost where it was', async () => {
    await section()
    for (const opener of ['.attach-orphan', '.delete-orphan']) {
      await w().find(opener).trigger('click')
      await flushPromises()
      dialog()?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      await flushPromises()
      expect(document.activeElement).toBe(w().find(opener).element)
    }
    expect(api.attachOrphanCost).not.toHaveBeenCalled()
    expect(api.deleteOrphanCost).not.toHaveBeenCalled()
  })

  it('speaks French', async () => {
    i18n.global.locale.value = 'fr'
    await section()
    expect(w().find('h2').text()).toBe('Coûts sans charge')
    expect(plain(w().find('.orphan h3').text())).toBe('Charge du 29 sept. 2026, 12:00 – 12:40')
  })
})
