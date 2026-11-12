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
  fetchVehicleState: () => new Promise(() => {}),
}))
// The Garage's reads: nothing to show, the page is what is tested.
vi.mock('vue-chartjs', () => import('@test/chart-stub'))
vi.mock('@/entities/stats', async (original) => ({
  ...(await original<typeof import('@/entities/stats')>()),
  fetchStats: () => new Promise(() => {}),
}))
vi.mock('@/entities/battery', async (original) => ({
  ...(await original<typeof import('@/entities/battery')>()),
  getBattery: () => new Promise(() => {}),
}))
vi.mock('@/entities/connection', async (original) => ({
  ...(await original<typeof import('@/entities/connection')>()),
  fetchConnection,
}))

// The flags source.tesla, source.bmw, source.pleos and source.skoda, as the session gives
// them: on unless a test turns one off.
const flag = vi.hoisted(() => ({ tesla: true, bmw: true, pleos: true, skoda: true }))
vi.mock('@/features/view-flags', async () => {
  const { computed } = await import('vue')
  return {
    useFlag: (name: string) =>
      computed(() =>
        name === 'source.bmw'
          ? flag.bmw
          : name === 'source.pleos'
            ? flag.pleos
            : name === 'source.skoda'
              ? flag.skoda
              : flag.tesla,
      ),
  }
})

// The extensions' choices and mentions: none in the public build, some in a test.
const ext = vi.hoisted(() => ({
  choices: [] as unknown[],
  mentions: null as Record<string, string> | null,
}))
vi.mock('@/pages/home/extensions', async () => {
  const { ref } = await import('vue')
  return {
    extensionChoices: ext.choices,
    get extensionMakeMentions() {
      const m = ext.mentions
      return m && (() => ref(m))
    },
  }
})

beforeEach(() => {
  ext.choices.length = 0
  ext.mentions = null
  flag.tesla = true
  flag.bmw = true
  flag.pleos = true
  flag.skoda = true
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
  it('goes to the only vehicle, in place of home', async () => {
    fetchVehicles.mockResolvedValue(vehicles.items.slice(0, 1))
    const { router } = await home()
    await vi.waitFor(() =>
      expect(router.currentRoute.value.fullPath).toBe(`/vehicles/${vehicles.items[0]?.id}`),
    )
    expect(window.history.length).toBe(1)
  })

  it('shows the Garage with several vehicles, and stays home', async () => {
    fetchVehicles.mockResolvedValue(vehicles.items)
    const { wrapper, router } = await home()
    // Loaded on demand.
    await vi.waitFor(() => expect(wrapper.find('.garage h1').exists()).toBe(true))
    expect(wrapper.find('.garage h1').text()).toBe('Garage')
    expect(wrapper.findAll('.vehicle-tile')).toHaveLength(vehicles.items.length)
    expect(router.currentRoute.value.name).toBe('home')
  })

  it('asks which car without a vehicle, one choice per make', async () => {
    fetchVehicles.mockResolvedValue([])
    const { wrapper, router } = await home()
    expect(wrapper.text()).toContain('No vehicle yet')
    expect(wrapper.text()).toContain('Which car is yours?')
    // Relative to <base>: a full page load of runsten-api's flow, under any prefix.
    const volvo = wrapper.find('a.volvo')
    expect(volvo.attributes('href')).toBe('auth/volvo/start')
    expect(volvo.text()).toBe('Volvo')
    // A Polestar's credential is given in the connection page's Polestar section.
    const polestar = wrapper.find('a.polestar')
    expect(polestar.attributes('href')).toBe('/connection#polestar')
    expect(polestar.text()).toBe('Polestar')
    // A Tesla's refresh token, in its own tab.
    const tesla = wrapper.find('a.tesla')
    expect(tesla.attributes('href')).toBe('/connection#tesla')
    expect(tesla.text()).toBe('Tesla')
    // A BMW's or a MINI's client ID, in BMW's tab: a tile each.
    const bmw = wrapper.find('a.bmw')
    expect(bmw.attributes('href')).toBe('/connection#bmw')
    expect(bmw.text()).toBe('BMW')
    const mini = wrapper.find('a.mini')
    expect(mini.attributes('href')).toBe('/connection#bmw')
    expect(mini.text()).toBe('MINI')
    // A Hyundai's, a Kia's, a Genesis's key, each in its make's tab: read through the same
    // API, but the owner looks for their make.
    const hyundai = wrapper.find('a.hyundai')
    expect(hyundai.attributes('href')).toBe('/connection#hyundai')
    expect(hyundai.text()).toBe('Hyundai, Beta')
    const kia = wrapper.find('a.kia')
    expect(kia.attributes('href')).toBe('/connection#kia')
    expect(kia.text()).toBe('Kia, Beta')
    const genesis = wrapper.find('a.genesis')
    expect(genesis.attributes('href')).toBe('/connection#genesis')
    expect(genesis.text()).toBe('Genesis, Beta')
    // A Škoda's key, in its tab, in beta.
    const skoda = wrapper.find('a.skoda')
    expect(skoda.attributes('href')).toBe('/connection#skoda')
    expect(skoda.text()).toBe('Škoda, Beta')
    // How each is read is told by its tab: the home only says it guides.
    expect(wrapper.text()).toContain(
      'Which car is yours? Choose its make: Runsten guides you through connecting it.',
    )
    expect(wrapper.text()).not.toContain('Pleos')
    // Tiles of equal weight, in a list named for screen readers.
    expect(wrapper.find('ul.makes').attributes('aria-label')).toBe("Your car's make")
    for (const b of [volvo, polestar, tesla, bmw, mini, hyundai, kia, genesis])
      expect(b.classes()).toContain('v-card--link')
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
    expect(polestar.text()).toBe('Polestar')
    expect(wrapper.find('a.tesla').attributes('href')).toBe('/connection#tesla')
  })

  it('leaves the Tesla out while its flag is off', async () => {
    flag.tesla = false
    fetchVehicles.mockResolvedValue([])
    const { wrapper } = await home()
    expect(wrapper.find('a.polestar').exists()).toBe(true)
    expect(wrapper.find('a.tesla').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Tesla')
  })

  it('leaves the BMW out while its flag is off', async () => {
    flag.bmw = false
    fetchVehicles.mockResolvedValue([])
    const { wrapper } = await home()
    expect(wrapper.find('a.tesla').exists()).toBe(true)
    expect(wrapper.find('a.bmw').exists()).toBe(false)
    expect(wrapper.find('a.mini').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('BMW')
  })

  it('leaves the Hyundai, Kia and Genesis out while their flag is off', async () => {
    flag.pleos = false
    fetchVehicles.mockResolvedValue([])
    const { wrapper } = await home()
    expect(wrapper.find('a.bmw').exists()).toBe(true)
    for (const make of ['hyundai', 'kia', 'genesis'])
      expect(wrapper.find(`a.${make}`).exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Kia')
  })

  it('leaves the Škoda out while its flag is off', async () => {
    flag.skoda = false
    fetchVehicles.mockResolvedValue([])
    const { wrapper } = await home()
    expect(wrapper.find('a.kia').exists()).toBe(true)
    expect(wrapper.find('a.skoda').exists()).toBe(false)
  })

  it("shows the extensions' choices after the makes, and their mentions", async () => {
    ext.choices.push({ template: '<button class="none-of-these">None of these</button>' })
    ext.mentions = { tesla: 'Plus offered', kia: 'Plus offered' }
    fetchVehicles.mockResolvedValue([])
    const { wrapper } = await home()
    expect(wrapper.find('a.tesla').text()).toBe('Tesla, Plus offered')
    expect(wrapper.find('a.polestar').text()).toBe('Polestar')
    // The mention takes Beta's place.
    expect(wrapper.find('a.kia').text()).toBe('Kia, Plus offered')
    expect(wrapper.find('a.hyundai').text()).toBe('Hyundai, Beta')
    const buttons = wrapper.findAll('.v-empty-state__actions a, .v-empty-state__actions button')
    expect(buttons.at(-1)?.classes()).toContain('none-of-these')
  })

  it('says when the vehicles cannot be loaded', async () => {
    fetchVehicles.mockRejectedValue(new ApiError(500, 'internal', ''))
    const { wrapper } = await home()
    expect(wrapper.find('.v-alert').text()).toContain('The vehicles could not be loaded')
  })
})
