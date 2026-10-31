import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SoftwareSummary } from '@/widgets/software-history'
import versions from '@fixtures/software-versions.json'
import state from '@fixtures/state-software.json'
import unknown from '@fixtures/state-unknown.json'
import { mountWith, testRouter } from '@test/utils'

const mocks = vi.hoisted(() => ({ fetchSoftwareVersions: vi.fn(), fetchVehicleState: vi.fn() }))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  fetchSoftwareVersions: mocks.fetchSoftwareVersions,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicleState: mocks.fetchVehicleState,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchSoftwareVersions.mockResolvedValue(versions)
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mounted(props: Record<string, unknown> = {}) {
  const router = testRouter()
  const { wrapper } = mountWith(SoftwareSummary, { router, props: { vehicle: 'v1', ...props } })
  await flushPromises()
  return { w: wrapper, router }
}

describe('SoftwareSummary', () => {
  it('tells the version running, since when, and leads to the history', async () => {
    const newest = versions.items[0]
    mocks.fetchVehicleState.mockResolvedValue({
      ...state,
      software: { ...state.software, value: newest?.version },
      software_update: null,
    })
    const { w, router } = await mounted()
    expect(w.find('h2').text()).toBe('Software')
    expect(w.find('dt').text()).toBe('Version running')
    expect(w.find('.value').text()).toBe('2026.38.3.2')
    expect(w.find('.when').text()).toBe('Installed 28 Sept 2026, 04:00–05:00')
    expect(w.find('.update').exists()).toBe(false)
    await w.find('a').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('software'))
    expect(router.currentRoute.value.params.vehicle).toBe('v1')
  })

  it('tells the update waiting, when set to install', async () => {
    mocks.fetchVehicleState.mockResolvedValue({
      ...state,
      software_update: {
        ...state.software_update,
        value: { ...state.software_update.value, status: 'scheduled' },
      },
    })
    const { w } = await mounted({ linked: false })
    // The state reads a version the history has not derived yet: no installation to tell.
    expect(w.find('.value').text()).toBe('2026.38.3.1')
    expect(w.find('.when').exists()).toBe(false)
    expect(w.find('.update dt').text()).toBe('Software update')
    expect(plain(w.find('.update dd').text())).toBe(
      '2026.38.3.2: Scheduled for 28 Sept 2026, 23:50',
    )
    // On the history's own page, under its title: the present, no link to it.
    expect(w.find('h2').text()).toBe('Now')
    expect(w.find('a').exists()).toBe(false)
  })

  it('shows nothing for a vehicle that tells no version', async () => {
    mocks.fetchVehicleState.mockResolvedValue(unknown)
    mocks.fetchSoftwareVersions.mockResolvedValue({ items: [], next_cursor: null })
    const { w } = await mounted()
    expect(w.find('.software-summary').exists()).toBe(false)
  })
})
