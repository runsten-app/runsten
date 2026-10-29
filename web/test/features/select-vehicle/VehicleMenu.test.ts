import { enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { VehicleMenu } from '@/features/select-vehicle'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

const { fetchVehicle, fetchVehicles } = vi.hoisted(() => ({
  fetchVehicle: vi.fn(),
  fetchVehicles: vi.fn(),
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicle,
  fetchVehicles,
}))

enableAutoUnmount(afterEach)

beforeEach(() => {
  fetchVehicle.mockReset()
  fetchVehicles.mockReset()
  fetchVehicles.mockResolvedValue(vehicles.items)
})

type Item = (typeof vehicles.items)[0]
const [first, second, third, fourth, fifth] = vehicles.items as [Item, Item, Item, Item, Item]

// opened mounts the menu on a route and opens it: its list is teleported to the body.
async function opened(path: string, vehicle = first.id) {
  const router = testRouter()
  await router.push(path)
  const { wrapper } = mountWith(VehicleMenu, {
    router,
    props: { vehicle },
    attachTo: document.body,
  })
  await flushPromises()
  const button = wrapper.find('button.vehicle-menu')
  await button.trigger('click')
  await flushPromises()
  const nav = [...document.querySelectorAll('nav')].at(-1)
  const rows = [...(nav?.querySelectorAll<HTMLAnchorElement>('a.vehicle-item') ?? [])]
  const settings = nav?.querySelector<HTMLAnchorElement>('a.vehicle-settings')
  return { wrapper, button, nav, rows, settings }
}

describe('VehicleMenu', () => {
  it('names the vehicle, lists the vehicles by their label and leads to its settings', async () => {
    const { button, nav, rows, settings } = await opened(`/vehicles/${first.id}`)
    expect(button.text()).toBe('XC40 Recharge Twin · 2021')
    expect(button.attributes('aria-label')).toBe('Vehicle: XC40 Recharge Twin · 2021')
    expect(nav?.getAttribute('aria-label')).toBe('Vehicle: XC40 Recharge Twin · 2021')
    expect(rows.map((a) => a.textContent?.trim())).toEqual([
      'XC40 Recharge Twin · 2021',
      second.vin, // details never read
      'EX30 Single Motor Extended Range · 2024', // chosen
      fourth.vin, // its market not served: never read
      'Polestar 2 Long Range Dual Motor / Performance · 2024', // chosen
    ])
    expect(rows.map((a) => a.getAttribute('aria-current'))).toEqual([
      'true',
      null,
      null,
      null,
      null,
    ])
    expect(rows[1]?.getAttribute('href')).toBe(`/vehicles/${second.id}`)
    expect(settings?.getAttribute('href')).toBe(`/vehicles/${first.id}/settings`)
    // Every row of the navigation's list is a listitem, the divider's too.
    const list = nav?.querySelector('[role="list"]')
    expect([...(list?.children ?? [])].every((c) => c.getAttribute('role') === 'listitem')).toBe(
      true,
    )
  })

  it.each([
    ['/health', '/health'],
    ['/battery', '/battery'],
    ['/trips?from=2026-09-01&to=2026-09-30', '/trips?from=2026-09-01&to=2026-09-30'],
    ['/trips/2026-09-28T07:01:00.000000Z?from=2026-09-01', '/trips?from=2026-09-01'],
    ['/charges/x?from=2026-09-01', '/charges?from=2026-09-01'],
    ['/stats?from=2026-09-01&view=costs', '/stats?from=2026-09-01'],
    ['/settings/history', '/settings/history'],
  ])('leads from %s to the same section of another vehicle', async (rest, to) => {
    const { rows } = await opened(`/vehicles/${first.id}${rest}`)
    expect(rows[1]?.getAttribute('href')).toBe(`/vehicles/${second.id}${to}`)
  })

  it("leads from the account's pages to the vehicles' state", async () => {
    const { rows } = await opened('/settings/integrations', third.id)
    expect(rows.map((a) => a.getAttribute('href'))).toEqual(
      [first, second, third, fourth, fifth].map((v) => `/vehicles/${v.id}`),
    )
  })

  it('tells apart two vehicles of the same model by their VIN', async () => {
    fetchVehicles.mockResolvedValue([first, { ...first, id: second.id, vin: 'YV1SMLT0000DT0742' }])
    const { rows } = await opened(`/vehicles/${first.id}`)
    expect(rows.map((a) => a.textContent?.trim())).toEqual([
      'XC40 Recharge Twin · 2021 · DT0001',
      'XC40 Recharge Twin · 2021 · DT0742',
    ])
  })

  // The pages under a vehicle rely on the bar to name it.
  it('names a single vehicle too', async () => {
    fetchVehicles.mockResolvedValue([first])
    const { button, rows, settings } = await opened(`/vehicles/${first.id}/settings`)
    expect(button.text()).toBe('XC40 Recharge Twin · 2021')
    expect(rows).toHaveLength(1)
    expect(settings?.getAttribute('href')).toBe(`/vehicles/${first.id}/settings`)
  })

  // The bar is the way to a vehicle's settings: a list that fails leaves it there.
  it('names the vehicle from its own read when the list fails, and leads to its settings', async () => {
    fetchVehicles.mockRejectedValue(new Error('down'))
    fetchVehicle.mockResolvedValue(first)
    const { button, rows, settings } = await opened(`/vehicles/${first.id}`)
    expect(fetchVehicle).toHaveBeenCalledWith(first.id)
    expect(button.text()).toBe('XC40 Recharge Twin · 2021')
    expect(rows).toHaveLength(0)
    expect(settings?.getAttribute('href')).toBe(`/vehicles/${first.id}/settings`)
  })

  it('reads no vehicle of its own while the list names it', async () => {
    await opened(`/vehicles/${first.id}`)
    expect(fetchVehicle).not.toHaveBeenCalled()
  })

  it('shows nothing before the vehicles are known', async () => {
    fetchVehicles.mockReturnValue(new Promise(() => {}))
    const router = testRouter()
    await router.push('/')
    const { wrapper } = mountWith(VehicleMenu, { router, props: { vehicle: first.id } })
    await flushPromises()
    expect(wrapper.find('button.vehicle-menu').exists()).toBe(false)
  })
})
