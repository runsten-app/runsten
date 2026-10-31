import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { HealthPage } from '@/pages/health'
import battery from '@fixtures/battery.json'
import health from '@fixtures/health.json'
import unknown from '@fixtures/health-unknown.json'
import noSoftware from '@fixtures/state-unknown.json'
import versions from '@fixtures/software-versions.json'
import tyres from '@fixtures/tyres.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const mocks = vi.hoisted(() => ({
  fetchHealth: vi.fn(),
  fetchSoftwareVersions: vi.fn(),
  fetchTyres: vi.fn(),
  fetchVehicles: vi.fn(),
  fetchVehicleState: vi.fn(),
  getBattery: vi.fn(),
}))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  fetchHealth: mocks.fetchHealth,
  fetchSoftwareVersions: mocks.fetchSoftwareVersions,
  fetchTyres: mocks.fetchTyres,
}))
vi.mock('@/entities/battery', async (original) => ({
  ...(await original<typeof import('@/entities/battery')>()),
  getBattery: mocks.getBattery,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
  fetchVehicleState: mocks.fetchVehicleState,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchHealth.mockResolvedValue(health)
  mocks.fetchSoftwareVersions.mockResolvedValue({ items: [], next_cursor: null })
  mocks.fetchTyres.mockResolvedValue(tyres)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
  // A state that tells no software: the versions alone tell it.
  mocks.fetchVehicleState.mockResolvedValue(noSoftware)
  mocks.getBattery.mockResolvedValue(battery)
})

const vehicle = vehicles.items[0]?.id ?? ''

async function mounted() {
  const router = testRouter()
  await router.push(`/vehicles/${vehicle}/health`)
  const { wrapper } = mountWith(HealthPage, { router, props: { vehicle } })
  await flushPromises()
  return wrapper
}

describe('HealthPage', () => {
  it("tells the software's version of a vehicle that tells it, nothing otherwise", async () => {
    let w = await mounted()
    expect(w.find('.software-summary').exists()).toBe(false)
    mocks.fetchSoftwareVersions.mockResolvedValue(versions)
    w = await mounted()
    expect(w.find('.software-summary h2').text()).toBe('Software')
    expect(w.find('.software-summary .value').text()).toBe('2026.38.3.2')
    // The history on a page of its own.
    expect(w.find('.software-summary a').attributes('href')).toBe(`/vehicles/${vehicle}/software`)
    expect(w.find('.version').exists()).toBe(false)
  })

  it('shows the tiles and the pressures, in its own tab', async () => {
    const w = await mounted()
    expect(w.find('.v-main h1').text()).toBe('Health')
    // The answer under the title: what needs attention, named, then how fresh it is.
    expect(w.find('.verdict').text()).toMatch(
      /^Needs attention: Rear left \(Low\), Rear right \(Fault\), Washer fluid\. · Last checked /,
    )
    expect(w.find('.health-tiles').exists()).toBe(true)
    // The pressures over time folded, their chart asked for once opened.
    expect(w.findAll('.chart-frame')).toHaveLength(0)
    expect(mocks.fetchTyres).not.toHaveBeenCalled()
    await w.find('.pressures button').trigger('click')
    await vi.waitFor(() => expect(w.findAll('.chart-frame')).toHaveLength(2))
    // The fixture needs attention: the dot's description follows the label.
    expect(w.find('.v-tab[aria-current="page"]').text()).toBe('Health Needs attention')
  })

  // The battery's capacity is derived from the charges: told whatever the vehicle told.
  it('leads to the battery’s page from its estimated capacity', async () => {
    mocks.fetchHealth.mockResolvedValue(unknown)
    const tile = (await mounted()).find('.battery-summary')
    expect(tile.find('h2').text()).toBe('Battery')
    expect(tile.find('dt').text()).toBe('Remaining capacity')
    expect(tile.find('a').attributes('href')).toBe(`/vehicles/${vehicle}/battery`)
  })

  it('says nothing needs attention only of what the vehicle reports', async () => {
    const fine = {
      ...health,
      tyres: { ...health.tyres, wheels: health.tyres.wheels.map((x) => ({ ...x, warning: 'ok' })) },
      checks: health.checks.map((c) => ({ ...c, warning: false })),
    }
    mocks.fetchHealth.mockResolvedValue(fine)
    expect((await mounted()).find('.verdict').text()).toMatch(
      /^Nothing the vehicle reports needs attention\. · Last/,
    )
    // Neither the service nor the checks reported (a Tesla): the same, of its tyres.
    mocks.fetchHealth.mockResolvedValue({ ...fine, service: null, checks: [] })
    expect((await mounted()).find('.verdict').text()).toMatch(
      /^Nothing the vehicle reports needs attention\. · Last/,
    )
    // A tyre without its monitoring's word: neither fine nor a warning, the freshness only.
    mocks.fetchHealth.mockResolvedValue({
      ...fine,
      tyres: { ...fine.tyres, wheels: fine.tyres.wheels.map((x) => ({ ...x, warning: null })) },
    })
    expect((await mounted()).find('.verdict').text()).toMatch(/^Last checked /)
  })

  it('draws no chart for a vehicle without pressures', async () => {
    mocks.fetchHealth.mockResolvedValue({
      ...health,
      tyres: {
        ...health.tyres,
        wheels: health.tyres.wheels.map((x) => ({ ...x, pressure_kpa: null })),
      },
    })
    const w = await mounted()
    expect(w.find('.health-tiles').exists()).toBe(true)
    expect(w.find('.pressures').exists()).toBe(false)
    expect(mocks.fetchTyres).not.toHaveBeenCalled()
  })

  it('says when nothing is known yet, and when the health cannot be read', async () => {
    mocks.fetchHealth.mockResolvedValue(unknown)
    expect((await mounted()).find('.v-main').text()).toContain('has told nothing of its health yet')
    mocks.fetchHealth.mockRejectedValue(new Error('down'))
    expect((await mounted()).find('.v-alert').text()).toContain('could not be loaded')
  })
})
