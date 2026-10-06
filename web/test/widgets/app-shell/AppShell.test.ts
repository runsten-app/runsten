import { enableAutoUnmount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { h } from 'vue'
import { AppShell } from '@/widgets/app-shell'
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

// The extensions' notices and menu items: none in the public build, some in a test.
const { notices, items } = vi.hoisted(() => ({ notices: [] as unknown[], items: [] as unknown[] }))
vi.mock('@/widgets/app-shell/extensions', () => ({
  extensionNotices: notices,
  extensionMenuItems: items,
}))

const display = vi.hoisted(() => ({ xs: false }))
vi.mock('vuetify', async (original) => {
  const v = await original<typeof import('vuetify')>()
  const { ref } = await import('vue')
  return { ...v, useDisplay: () => ({ ...v.useDisplay(), xs: ref(display.xs) }) }
})

// A shell left mounted would keep listening, and take the keys first.
enableAutoUnmount(afterEach)

beforeEach(() => {
  notices.length = 0
  items.length = 0
  display.xs = false
  fetchVehicle.mockReset()
  fetchVehicle.mockResolvedValue(vehicles.items[0])
  fetchVehicles.mockReset()
  fetchVehicles.mockResolvedValue(vehicles.items)
})

// menuItem opens the user menu and gives its item: the menu is teleported to the body.
async function menuItem(wrapper: VueWrapper, name: string) {
  await wrapper.find('button.user-menu').trigger('click')
  await flushPromises()
  return [...document.querySelectorAll<HTMLAnchorElement>(`a.${name}`)].at(-1)
}

async function shell(props: { vehicle?: string } = {}, path = '/', item = 'settings') {
  const router = testRouter()
  await router.push(path)
  const { wrapper } = mountWith(AppShell, { props, router, attachTo: document.body })
  await flushPromises()
  return { wrapper, link: await menuItem(wrapper, item) }
}

describe('AppShell', () => {
  // The account's settings belong to no vehicle.
  it('leads to the settings from the user menu, keeping no vehicle', async () => {
    const { link } = await shell({ vehicle: 'v1' })
    expect(link?.getAttribute('href')).toBe('/settings')
    expect(link?.querySelector('.v-list-item-title')?.textContent).toBe('Settings')
  })

  it('leads to the connection from the user menu, keeping the vehicle', async () => {
    const { link } = await shell({ vehicle: 'v1' }, '/', 'connection')
    expect(link?.getAttribute('href')).toBe('/connection?vehicle=v1')
    expect(link?.querySelector('.v-list-item-title')?.textContent).toBe('Connection')
  })

  it('keeps the vehicle within the connection, on a phone too', async () => {
    display.xs = true
    const { link } = await shell({}, '/connection?vehicle=v1', 'connection')
    expect(link?.getAttribute('href')).toBe('/connection?vehicle=v1')
  })

  it('puts a dot on the menu and on the connection when a vehicle is not read', async () => {
    const { wrapper, link } = await shell({}, '/', 'connection')
    const label = 'The connection needs your attention'
    const button = wrapper.find('button.user-menu')
    const description = button.attributes('aria-describedby')
    expect(document.getElementById(String(description))?.textContent).toBe(label)
    expect(link?.querySelector('.attention-dot')?.textContent).toBe(label)
  })

  // Before the collector's first pass, nothing failed yet.
  it('puts no dot while no vehicle needs the user', async () => {
    fetchVehicles.mockResolvedValue([vehicles.items[2]])
    const { wrapper } = await shell({}, '/', 'connection')
    expect(wrapper.find('button.user-menu').attributes('aria-describedby')).toBeUndefined()
    expect(document.querySelector('.attention-dot')).toBeNull()
  })

  it('warns on the pages of a vehicle that nothing new is read', async () => {
    fetchVehicle.mockResolvedValue(vehicles.items[1])
    const router = testRouter()
    await router.push('/')
    const { wrapper } = mountWith(AppShell, { props: { vehicle: vehicles.items[1]?.id }, router })
    await flushPromises()
    expect(fetchVehicle).toHaveBeenCalledWith(vehicles.items[1]?.id)
    expect(wrapper.find('.collection-notice').text()).toContain('Re-authentication required')
  })

  it("tells the extensions' notices first, on every page", async () => {
    notices.push(
      { render: () => h('p', { class: 'extension-notice' }, 'Pay') },
      { render: () => null },
    )
    fetchVehicle.mockResolvedValue(vehicles.items[1])
    const router = testRouter()
    await router.push('/')
    const { wrapper } = mountWith(AppShell, { props: { vehicle: vehicles.items[1]?.id }, router })
    await flushPromises()
    const shell = wrapper.find('.shell')
    expect(shell.find('.extension-notice').text()).toBe('Pay')
    expect(shell.element.firstElementChild?.className).toBe('extension-notice')
  })

  it("lists the extensions' items in the user menu, after the connection", async () => {
    items.push({ render: () => h('a', { class: 'v-list-item extension-item', href: '/plans' }) })
    const { link } = await shell({}, '/', 'extension-item')
    expect(link?.getAttribute('href')).toBe('/plans')
    expect(link?.previousElementSibling?.classList).toContain('connection')
  })

  it('warns of nothing without a vehicle', async () => {
    await shell()
    expect(fetchVehicle).not.toHaveBeenCalled()
  })
})

describe('AppShell shortcuts', () => {
  it('names the key of each tab, and shows them while Shift is held', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const router = testRouter()
    await router.push('/vehicles/v1')
    const { wrapper } = mountWith(AppShell, { props: { vehicle: 'v1' }, router })
    await flushPromises()
    const tabs = wrapper.findAll('nav a')
    expect(tabs.map((a) => a.attributes('aria-keyshortcuts'))).toEqual(['1', '2', '3', '4', '5'])
    expect(wrapper.find('.key-hint').exists()).toBe(false)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Shift', code: 'ShiftLeft' }))
    vi.runAllTimers()
    await flushPromises()
    expect(wrapper.findAll('.key-hint').map((k) => k.text())).toEqual(['1', '2', '3', '4', '5'])
    // The tab's name stays its label alone.
    expect(tabs[1]?.find('.key-hint').attributes('aria-hidden')).toBe('true')
    wrapper.unmount()
    vi.useRealTimers()
  })

  it('names the key of the settings in the menu', async () => {
    const { link } = await shell()
    expect(link?.getAttribute('aria-keyshortcuts')).toBe(',')
    expect(link?.querySelector('.menu-key')?.getAttribute('aria-hidden')).toBe('true')
  })
})

describe('AppShell shortcuts beyond a vehicle', () => {
  const press = (key: string, code: string) =>
    window.dispatchEvent(new KeyboardEvent('keydown', { key, code, cancelable: true }))

  it('lead back to the first vehicle from the settings', async () => {
    const router = testRouter()
    await router.push('/settings')
    mountWith(AppShell, { router })
    await flushPromises()
    press('1', 'Digit1')
    await vi.waitFor(() =>
      expect(router.currentRoute.value.fullPath).toBe(`/vehicles/${vehicles.items[0]?.id}`),
    )
  })

  it('lead to the vehicle the connection was opened from', async () => {
    const router = testRouter()
    await router.push('/connection?vehicle=v2')
    mountWith(AppShell, { router })
    await flushPromises()
    press('3', 'Digit3')
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/vehicles/v2/trips'))
  })
})
