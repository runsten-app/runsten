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

async function page(vehicle: string) {
  const router = testRouter()
  await router.push(`/vehicles/${vehicle}/settings`)
  const { wrapper } = mountWith(VehicleSettingsPage, { router, props: { vehicle } })
  await flushPromises()
  return wrapper
}

describe('VehicleSettingsPage', () => {
  it("has the vehicle's model form, in the vehicle's shell", async () => {
    mocks.fetchVehicle.mockResolvedValue(ex30)
    const w = await page(ex30?.id ?? '')
    expect(w.find('.v-main h1').text()).toBe('Vehicle settings')
    expect(w.find('.vehicle-model h2').text()).toBe('EX30 Single Motor Extended Range · 2024')
    expect(w.find('.vehicle-model form').exists()).toBe(true)
    expect(mocks.fetchVariants).toHaveBeenCalledWith(ex30?.id)
    // The vehicle's tabs, none of them this page's.
    expect(w.findAll('.v-tab')).toHaveLength(5)
    expect(w.find('.v-tab[aria-current="page"]').exists()).toBe(false)
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
