import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Vehicle } from '@/entities/vehicle'
import { VehicleAliasForm } from '@/features/name-vehicle'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import named from '@fixtures/vehicle-alias.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({ setVehicleAlias: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  ...api,
}))

beforeEach(() => {
  api.setVehicleAlias.mockReset()
  i18n.global.locale.value = 'en'
})
// Mounted in the document, for the focus.
const mounted: VueWrapper[] = []
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount())
})

// The XC40 of the fixtures, without an alias, and the same named.
const xc40 = vehicles.items[0] as Vehicle
const pool = named as Vehicle

function form(vehicle: Vehicle) {
  const m = mountWith(VehicleAliasForm, { props: { vehicle }, attachTo: document.body })
  mounted.push(m.wrapper)
  return m.wrapper
}
const input = (w: VueWrapper) => w.find<HTMLInputElement>('input')
async function type(w: VueWrapper, text: string) {
  await input(w).setValue(text)
  await flushPromises()
}
async function save(w: VueWrapper) {
  const button = w.find<HTMLButtonElement>('button.save')
  button.element.focus()
  await w.find('form').trigger('submit')
  await flushPromises()
  return button.element
}

describe('VehicleAliasForm', () => {
  it('is a tile with its heading, the label without an alias in its hint', () => {
    const w = form(pool)
    expect(w.find('h3').text()).toBe('Name')
    expect(input(w).element.value).toBe('Fleet car 12')
    const hint = w.find(`#${input(w).attributes('aria-describedby')?.split(' ')[0]}`)
    expect(hint.text()).toContain('Leave it empty to go back to XC40 Recharge Twin · 2021.')
  })

  it('sends the alias trimmed, and says so where the focus stays', async () => {
    api.setVehicleAlias.mockResolvedValue(named)
    const w = form(xc40)
    expect(input(w).element.value).toBe('')
    await type(w, '  Fleet car 12 ')
    const button = await save(w)
    expect(api.setVehicleAlias).toHaveBeenCalledWith(xc40.id, { alias: 'Fleet car 12' })
    expect(input(w).element.value).toBe('Fleet car 12')
    expect(w.find('[role="status"]').text()).toBe('Name saved.')
    expect(document.activeElement).toBe(button)
    // A change clears the message.
    await type(w, 'Fleet car 13')
    expect(w.find('[role="status"]').text()).toBe('')
  })

  it('removes the alias when emptied', async () => {
    api.setVehicleAlias.mockResolvedValue(xc40)
    const w = form(pool)
    await type(w, ' ')
    await save(w)
    expect(api.setVehicleAlias).toHaveBeenCalledWith(pool.id, { alias: null })
    expect(w.find('[role="status"]').text()).toBe('Name removed.')
  })

  it('refuses a name too long, in a summary that leads to it', async () => {
    const w = form(xc40)
    await type(w, 'a'.repeat(41))
    await save(w)
    expect(api.setVehicleAlias).not.toHaveBeenCalled()
    const summary = w.find('.error-summary')
    expect(document.activeElement).toBe(summary.element)
    expect(summary.text()).toContain('The name could not be saved')
    expect(summary.find('li').text()).toBe('At most 40 characters.')
    expect(input(w).attributes('aria-invalid')).toBe('true')
    await summary.find('a').trigger('click')
    expect(document.activeElement).toBe(input(w).element)
    // Shortened, the error goes.
    await type(w, 'a'.repeat(40))
    expect(w.find('.error-summary').exists()).toBe(false)
  })

  it('refuses a control character', async () => {
    const w = form(xc40)
    await type(w, 'Pool\tcar')
    await save(w)
    expect(w.find('.error-summary li').text()).toBe('The name cannot hold a line break or a tab.')
  })

  it.each([
    [
      new ApiError(400, 'invalid_body', 'body.alias: too long'),
      'Runsten refused the name: body.alias: too long',
    ],
    [new ApiError(404, 'not_found', ''), 'This vehicle no longer exists.'],
    [new ApiError(0, 'network', ''), 'Runsten cannot be reached. Try again in a moment.'],
    [new ApiError(500, 'internal', ''), 'Something went wrong. Try again later.'],
  ])('tells a refusal, %o, in the summary', async (error, text) => {
    api.setVehicleAlias.mockRejectedValue(error)
    const w = form(xc40)
    await type(w, 'Mine')
    await save(w)
    const summary = w.find('.error-summary')
    expect(summary.find('li').text()).toBe(text)
    expect(document.activeElement).toBe(summary.element)
    expect(w.find('[role="status"]').text()).toBe('')
  })
})
