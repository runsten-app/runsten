import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { RouterView } from 'vue-router'
import { PlacePage } from '@/pages/place'
import { ApiError } from '@/shared/api'
import place from '@fixtures/place.json'
import settings from '@fixtures/settings.json'
import state from '@fixtures/state.json'
import unknown from '@fixtures/state-unknown.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

const mocks = vi.hoisted(() => ({
  fetchPlace: vi.fn(),
  createPlace: vi.fn(),
  deletePlace: vi.fn(),
  fetchSettings: vi.fn(),
  fetchVehicles: vi.fn(),
  fetchVehicleState: vi.fn(),
}))
vi.mock('@/entities/place', async (original) => ({
  ...(await original<typeof import('@/entities/place')>()),
  fetchPlace: mocks.fetchPlace,
  createPlace: mocks.createPlace,
  deletePlace: mocks.deletePlace,
}))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  fetchSettings: mocks.fetchSettings,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
  fetchVehicleState: mocks.fetchVehicleState,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchSettings.mockResolvedValue(settings)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
  // The first vehicle has a position, the second none.
  mocks.fetchVehicleState.mockImplementation((v: string) =>
    Promise.resolve(v === vehicles.items[0]?.id ? state : unknown),
  )
})

// page opens the app's route of a place, props as the router gives them.
async function page(path: string) {
  const router = testRouter([
    {
      path: '/settings/places/:place',
      name: 'place',
      component: PlacePage,
      props: (to) => ({
        place: to.params.place === 'new' ? undefined : String(to.params.place),
      }),
    },
  ])
  await router.push(path)
  const m = mountWith(RouterView, { router, attachTo: document.body })
  await flushPromises()
  return m
}

describe('PlacePage', () => {
  it('edits a place, titled by its name', async () => {
    mocks.fetchPlace.mockResolvedValue(place)
    const { wrapper } = await page(`/settings/places/${place.id}`)
    expect(mocks.fetchPlace).toHaveBeenCalledWith(place.id)
    expect(wrapper.find('h1').text()).toBe('Maison de Tante Agathe')
    expect(wrapper.find<HTMLInputElement>('#place-name').element.value).toBe(
      'Maison de Tante Agathe',
    )
    expect(wrapper.find('a.back').attributes('href')).toBe('/settings')
    wrapper.unmount()
  })

  it('offers the position of each vehicle that has one', async () => {
    const { wrapper } = await page('/settings/places/new')
    expect(wrapper.find('h1').text()).toBe('New place')
    const buttons = wrapper.findAll('.vehicle-position')
    expect(buttons).toHaveLength(1)
    expect(buttons[0]?.text()).toMatch(/^Current position of /)
    await buttons[0]?.trigger('click')
    await flushPromises()
    expect(wrapper.find<HTMLInputElement>('#place-lat').element.value).toBe('45.76400')
    expect(wrapper.find('[role="status"]').text()).toMatch(/^Position of .+ filled in\.$/)
    wrapper.unmount()
  })

  it('takes the ID of a new place once saved, keeping the form and its focus', async () => {
    mocks.createPlace.mockResolvedValue(place)
    // A refetch of the saved place (the tests' cache is stale at once) leaves the form be.
    mocks.fetchPlace.mockResolvedValue({ ...place, name: 'Renamed elsewhere' })
    const { wrapper, router } = await page('/settings/places/new')
    await wrapper.find('#place-name').setValue(place.name)
    await wrapper.find('#place-lat').setValue('45.764')
    await wrapper.find('#place-lon').setValue('4.8357')
    await wrapper.find('#place-v0-price').setValue('0.2142')
    const form = wrapper.find('form').element
    const save = wrapper.find<HTMLButtonElement>('button.save').element
    save.focus()
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    await vi.waitFor(() =>
      expect(router.currentRoute.value.fullPath).toBe(`/settings/places/${place.id}`),
    )
    await flushPromises()
    expect(wrapper.find('form').element).toBe(form)
    expect(wrapper.find<HTMLInputElement>('#place-name').element.value).toBe(place.name)
    expect(document.activeElement).toBe(save)
    expect(wrapper.find('[role="status"]').text()).toBe('Place saved.')
    wrapper.unmount()
  })

  it('goes back to the settings once the place is deleted', async () => {
    mocks.fetchPlace.mockResolvedValue(place)
    mocks.deletePlace.mockResolvedValue(undefined)
    const { wrapper, router } = await page(`/settings/places/${place.id}`)
    await wrapper.find('.delete-place').trigger('click')
    await flushPromises()
    ;(document.querySelector('.confirm-delete') as HTMLElement | null)?.click()
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/settings'))
    wrapper.unmount()
  })

  // The form checks the limits of the settings: it waits for them, never checks others.
  it('opens the form once the settings are there', async () => {
    mocks.fetchSettings.mockReturnValue(new Promise(() => {}))
    const { wrapper } = await page('/settings/places/new')
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.find('[role="progressbar"]').attributes('aria-label')).toBe('Loading the place')
    wrapper.unmount()
  })

  it('tells a failure of the settings', async () => {
    mocks.fetchSettings.mockRejectedValue(new ApiError(500, 'internal', ''))
    const { wrapper } = await page('/settings/places/new')
    expect(wrapper.find('.v-alert').text()).toBe(
      'The place could not be loaded. Try again in a moment.',
    )
    expect(wrapper.find('form').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    [new ApiError(404, 'not_found', ''), 'This place does not exist.'],
    [new ApiError(500, 'internal', ''), 'The place could not be loaded. Try again in a moment.'],
  ])('tells %o', async (error, text) => {
    mocks.fetchPlace.mockRejectedValue(error)
    const { wrapper } = await page('/settings/places/nope')
    expect(wrapper.find('.v-alert').text()).toBe(text)
    expect(wrapper.find('form').exists()).toBe(false)
    wrapper.unmount()
  })
})
