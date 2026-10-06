import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { HomePage } from '@/pages/home'
import { ApiError } from '@/shared/api'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

const { fetchVehicles } = vi.hoisted(() => ({ fetchVehicles: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles,
}))

beforeEach(() => {
  fetchVehicles.mockReset()
})

async function home() {
  const router = testRouter([
    { path: '/vehicles/:vehicle', name: 'vehicle', component: { render: () => null } },
  ])
  await router.push('/')
  const { wrapper } = mountWith(HomePage, { router })
  await flushPromises()
  return { wrapper, router }
}

describe('HomePage', () => {
  it('goes to the first vehicle, in place of home', async () => {
    fetchVehicles.mockResolvedValue(vehicles.items)
    const { router } = await home()
    await vi.waitFor(() =>
      expect(router.currentRoute.value.fullPath).toBe(`/vehicles/${vehicles.items[0]?.id}`),
    )
    expect(window.history.length).toBe(1)
  })

  it('offers to connect a Volvo ID without a vehicle', async () => {
    fetchVehicles.mockResolvedValue([])
    const { wrapper, router } = await home()
    expect(wrapper.text()).toContain('No vehicle yet')
    // Relative to <base>: a full page load of runsten-api's flow, under any prefix.
    const link = wrapper.find('a[href="auth/volvo/start"]')
    expect(link.text()).toBe('Connect a Volvo ID')
    expect(router.currentRoute.value.name).toBe('home')
  })

  it('says when the vehicles cannot be loaded', async () => {
    fetchVehicles.mockRejectedValue(new ApiError(500, 'internal', ''))
    const { wrapper } = await home()
    expect(wrapper.find('.v-alert').text()).toContain('The vehicles could not be loaded')
  })
})
