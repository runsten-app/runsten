import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { HomePage } from '@/pages/home'
import { ApiError } from '@/shared/api'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

const { fetchVehicles, fetchConnection } = vi.hoisted(() => ({
  fetchVehicles: vi.fn(),
  fetchConnection: vi.fn(),
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles,
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  fetchConnection,
}))

// The flag source.tesla, as the session gives it: on unless a test turns it off.
const flag = vi.hoisted(() => ({ tesla: true }))
vi.mock('@/features/view-flags', async () => {
  const { computed } = await import('vue')
  return { useFlag: () => computed(() => flag.tesla) }
})

beforeEach(() => {
  flag.tesla = true
  fetchVehicles.mockReset()
  fetchConnection.mockReset()
  // Self-hosting: the instance's key reads the vehicles.
  fetchConnection.mockResolvedValue({
    volvo: true,
    connected: false,
    client_id: 'runsten-dev',
    instance_key: { last4: 'e123' },
    api_key: null,
  })
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

  it('asks which car without a vehicle, Volvo, Polestar or Tesla', async () => {
    fetchVehicles.mockResolvedValue([])
    const { wrapper, router } = await home()
    expect(wrapper.text()).toContain('No vehicle yet')
    expect(wrapper.text()).toContain('Which car is yours?')
    // Relative to <base>: a full page load of runsten-api's flow, under any prefix.
    const volvo = wrapper.find('a.volvo')
    expect(volvo.attributes('href')).toBe('auth/volvo/start')
    expect(volvo.text()).toBe('I have a Volvo')
    // A Polestar's credential is given in the connection page's Polestar section.
    const polestar = wrapper.find('a.polestar')
    expect(polestar.attributes('href')).toBe('/connection#polestar')
    expect(polestar.text()).toBe('I have a Polestar (beta)')
    // A Tesla's refresh token, in its own tab.
    const tesla = wrapper.find('a.tesla')
    expect(tesla.attributes('href')).toBe('/connection#tesla')
    expect(tesla.text()).toBe('I have a Tesla (beta)')
    // Choices of equal weight.
    for (const b of [volvo, polestar, tesla]) expect(b.classes()).toContain('v-btn--variant-flat')
    expect(router.currentRoute.value.name).toBe('home')
  })

  it('leads a Volvo to the key first on an instance without its own', async () => {
    fetchVehicles.mockResolvedValue([])
    fetchConnection.mockResolvedValue({
      volvo: true,
      connected: false,
      client_id: 'runsten-dev',
      instance_key: null,
      api_key: null,
      polestar: null,
    })
    const { wrapper } = await home()
    expect(wrapper.text()).toContain('the key of a Volvo application you create')
    expect(wrapper.text()).toContain("a Polestar with a credential of Polestar's Data Portal")
    expect(wrapper.find('a[href="auth/volvo/start"]').exists()).toBe(false)
    expect(wrapper.find('a.volvo').attributes('href')).toBe('/connection')
    expect(wrapper.find('a.polestar').attributes('href')).toBe('/connection#polestar')
  })

  it('leaves the Volvo out on an instance without a Volvo application', async () => {
    fetchVehicles.mockResolvedValue([])
    fetchConnection.mockResolvedValue({
      volvo: false,
      connected: false,
      client_id: '',
      instance_key: null,
      api_key: null,
      polestar: null,
    })
    const { wrapper } = await home()
    expect(wrapper.text()).toContain('This instance is not set up to read a Volvo.')
    expect(wrapper.find('.volvo').exists()).toBe(false)
    const polestar = wrapper.find('a.polestar')
    expect(polestar.attributes('href')).toBe('/connection#polestar')
    expect(polestar.text()).toBe('I have a Polestar (beta)')
    expect(wrapper.find('a.tesla').attributes('href')).toBe('/connection#tesla')
  })

  it('leaves the Tesla out while its flag is off', async () => {
    flag.tesla = false
    fetchVehicles.mockResolvedValue([])
    const { wrapper } = await home()
    expect(wrapper.find('a.polestar').exists()).toBe(true)
    expect(wrapper.find('a.tesla').exists()).toBe(false)
  })

  it('says when the vehicles cannot be loaded', async () => {
    fetchVehicles.mockRejectedValue(new ApiError(500, 'internal', ''))
    const { wrapper } = await home()
    expect(wrapper.find('.v-alert').text()).toContain('The vehicles could not be loaded')
  })
})
