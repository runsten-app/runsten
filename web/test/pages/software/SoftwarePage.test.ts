import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SoftwarePage } from '@/pages/software'
import versions from '@fixtures/software-versions.json'
import state from '@fixtures/state-software.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

const mocks = vi.hoisted(() => ({
  fetchSoftwareVersions: vi.fn(),
  fetchVehicles: vi.fn(),
  fetchVehicleState: vi.fn(),
}))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  fetchSoftwareVersions: mocks.fetchSoftwareVersions,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
  fetchVehicleState: mocks.fetchVehicleState,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchSoftwareVersions.mockResolvedValue(versions)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
  mocks.fetchVehicleState.mockResolvedValue(state)
})

const vehicle = vehicles.items[0]?.id ?? ''

describe('SoftwarePage', () => {
  // A part of the health, as the battery's page: the health's tab is current.
  it('shows the version running, then the history, in the health’s tab', async () => {
    const router = testRouter()
    await router.push(`/vehicles/${vehicle}/software`)
    const { wrapper } = mountWith(SoftwarePage, { router, props: { vehicle } })
    await flushPromises()
    expect(wrapper.find('.v-main h1').text()).toBe('Software')
    expect(wrapper.find('.software-summary a').exists()).toBe(false)
    expect(wrapper.findAll('.software-versions .version')).toHaveLength(3)
    expect(wrapper.find('.v-tab[aria-current="page"]').text()).toMatch(/^Health/)
  })
})
