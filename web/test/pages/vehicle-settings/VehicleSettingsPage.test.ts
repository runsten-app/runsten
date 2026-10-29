import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { VehicleSettingsPage } from '@/pages/vehicle-settings'
import { ApiError } from '@/shared/api'
import variants from '@fixtures/variants.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

const mocks = vi.hoisted(() => ({
  fetchVehicle: vi.fn(),
  fetchVehicles: vi.fn(),
  fetchVariants: vi.fn(),
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  ...mocks,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
  mocks.fetchVariants.mockResolvedValue(variants.items)
})

// The EX30 of the fixtures, the one with variants to choose from.
const ex30 = vehicles.items[2]

async function page(vehicle: string, area: 'model' | 'history' = 'model') {
  const router = testRouter()
  await router.push(`/vehicles/${vehicle}/settings${area === 'history' ? '/history' : ''}`)
  const { wrapper } = mountWith(VehicleSettingsPage, { router, props: { vehicle, area } })
  await flushPromises()
  return wrapper
}

describe('VehicleSettingsPage', () => {
  it("has the vehicle's model form, in the vehicle's shell", async () => {
    mocks.fetchVehicle.mockResolvedValue(ex30)
    const w = await page(ex30?.id ?? '')
    expect(w.find('.v-main h1').text()).toBe('Vehicle settings')
    expect(w.find('.area h2').text()).toBe('Model and charger')
    expect(w.find('.vehicle-model form').exists()).toBe(true)
    expect(w.find('.import-list').exists()).toBe(false)
    expect(mocks.fetchVariants).toHaveBeenCalledWith(ex30?.id)
    // The vehicle's tabs, none of them this page's.
    expect(w.findAll('.v-tab')).toHaveLength(5)
    expect(w.find('.v-tab[aria-current="page"]').exists()).toBe(false)
  })

  it.each([
    ['model', 'Model and charger'],
    ['history', 'Imported history'],
  ] as const)('leads to its areas, %s the one shown', async (area, shown) => {
    mocks.fetchVehicle.mockResolvedValue(ex30)
    const w = await page(ex30?.id ?? '', area)
    const nav = w.find('nav.sub-nav')
    expect(nav.attributes('aria-label')).toBe('Vehicle settings areas')
    const links = nav.findAll('a')
    expect(links.map((a) => [a.text(), a.attributes('href')])).toEqual([
      ['Model and charger', `/vehicles/${ex30?.id}/settings`],
      ['Imported history', `/vehicles/${ex30?.id}/settings/history`],
    ])
    expect(nav.find('[aria-current="page"]').text()).toBe(shown)
    expect(w.find('.area h2').text()).toBe(shown)
    expect(w.find('.area').attributes('aria-labelledby')).toBe(w.find('.area h2').attributes('id'))
  })

  it('lists what was imported before the ways to import more', async () => {
    mocks.fetchVehicle.mockResolvedValue(ex30)
    const w = await page(ex30?.id ?? '', 'history')
    const tiles = w.findAll('.history > section')
    expect(tiles.map((s) => s.classes().includes('import-list'))).toEqual([true, false])
    expect(tiles[1]?.classes()).toContain('import-sources')
    expect(w.find('.vehicle-model').exists()).toBe(false)
  })

  it.each([
    ['a Volvo', ex30, true],
    ['a Polestar', vehicles.items[4], false],
  ])("offers the Volvo Data Portal's files to %s only, Aximote to both", async (_, v, portal) => {
    mocks.fetchVehicle.mockResolvedValue(v)
    const w = await page(v?.id ?? '', 'history')
    expect(w.find('.aximote-import').exists()).toBe(true)
    expect(w.find('.portal-import').exists()).toBe(portal)
  })

  it.each([
    [new ApiError(404, 'not_found', ''), 'This vehicle does not exist.'],
    [new ApiError(500, 'internal', ''), 'The vehicle could not be loaded. Try again in a moment.'],
  ])('tells a failure: %s', async (e, text) => {
    mocks.fetchVehicle.mockRejectedValue(e)
    const w = await page('nope')
    expect(w.find('.v-main .v-alert').text()).toBe(text)
    expect(w.find('.vehicle-model').exists()).toBe(false)
  })
})
