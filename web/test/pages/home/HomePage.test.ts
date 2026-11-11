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

// The flags source.tesla, source.bmw and source.pleos, as the session gives them: on
// unless a test turns one off.
const flag = vi.hoisted(() => ({ tesla: true, bmw: true, pleos: true }))
vi.mock('@/features/view-flags', async () => {
  const { computed } = await import('vue')
  return {
    useFlag: (name: string) =>
      computed(() =>
        name === 'source.bmw' ? flag.bmw : name === 'source.pleos' ? flag.pleos : flag.tesla,
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

  it('asks which car without a vehicle, Volvo, Polestar, Tesla or BMW', async () => {
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
    expect(polestar.text()).toBe('I have a Polestar')
    // A Tesla's refresh token, in its own tab.
    const tesla = wrapper.find('a.tesla')
    expect(tesla.attributes('href')).toBe('/connection#tesla')
    expect(tesla.text()).toBe('I have a Tesla')
    // A BMW's or a MINI's client ID, in its own tab.
    const bmw = wrapper.find('a.bmw')
    expect(bmw.attributes('href')).toBe('/connection#bmw')
    expect(bmw.text()).toBe('I have a BMW or MINI')
    // A Hyundai's, Kia's or Genesis's key, in the tab of the three.
    const pleos = wrapper.find('a.pleos')
    expect(pleos.attributes('href')).toBe('/connection#pleos')
    expect(pleos.text()).toBe('I have a Hyundai, Kia or Genesis')
    // How each is read, listed as English lists.
    expect(wrapper.text()).toContain(
      "Runsten reads a Volvo through your Volvo ID, a Polestar with a credential of Polestar's Data Portal, a Tesla with a refresh token of your Tesla account, a BMW or a MINI with a client ID you create in the MyBMW or MyMINI portal, and a Hyundai, a Kia or a Genesis with a key you create in the Pleos Playground.",
    )
    // Choices of equal weight.
    for (const b of [volvo, polestar, tesla, bmw, pleos])
      expect(b.classes()).toContain('v-btn--variant-flat')
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
    expect(polestar.text()).toBe('I have a Polestar')
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
    expect(wrapper.text()).not.toContain('BMW')
  })

  it('leaves the Hyundai, Kia and Genesis out while their flag is off', async () => {
    flag.pleos = false
    fetchVehicles.mockResolvedValue([])
    const { wrapper } = await home()
    expect(wrapper.find('a.bmw').exists()).toBe(true)
    expect(wrapper.find('a.pleos').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Kia')
  })

  it("shows the extensions' choices after the makes, and their mentions", async () => {
    ext.choices.push({ template: '<button class="none-of-these">None of these</button>' })
    ext.mentions = { tesla: 'Plus offered', pleos: 'Beta' }
    fetchVehicles.mockResolvedValue([])
    const { wrapper } = await home()
    expect(wrapper.find('a.tesla').text()).toBe('I have a Tesla · Plus offered')
    expect(wrapper.find('a.polestar').text()).toBe('I have a Polestar')
    expect(wrapper.find('a.pleos').text()).toBe('I have a Hyundai, Kia or Genesis · Beta')
    const buttons = wrapper.findAll('.v-empty-state__actions a, .v-empty-state__actions button')
    expect(buttons.at(-1)?.classes()).toContain('none-of-these')
  })

  it('says when the vehicles cannot be loaded', async () => {
    fetchVehicles.mockRejectedValue(new ApiError(500, 'internal', ''))
    const { wrapper } = await home()
    expect(wrapper.find('.v-alert').text()).toContain('The vehicles could not be loaded')
  })
})
