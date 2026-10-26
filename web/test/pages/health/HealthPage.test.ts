import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { HealthPage } from '@/pages/health'
import health from '@fixtures/health.json'
import unknown from '@fixtures/health-unknown.json'
import tyres from '@fixtures/tyres.json'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testRouter } from '@test/utils'

vi.mock('vue-chartjs', () => import('@test/chart-stub'))
const mocks = vi.hoisted(() => ({
  fetchHealth: vi.fn(),
  fetchTyres: vi.fn(),
  fetchVehicles: vi.fn(),
}))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  fetchHealth: mocks.fetchHealth,
  fetchTyres: mocks.fetchTyres,
}))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicles: mocks.fetchVehicles,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
  mocks.fetchHealth.mockResolvedValue(health)
  mocks.fetchTyres.mockResolvedValue(tyres)
  mocks.fetchVehicles.mockResolvedValue(vehicles.items)
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
  it('shows the tiles and the pressures, in the vehicle’s tab', async () => {
    const w = await mounted()
    expect(w.find('.v-main h1').text()).toBe('Health')
    expect(w.find('.intro').text()).toContain('a Volvo gives a warning per tyre')
    expect(w.find('.health-tiles').exists()).toBe(true)
    await vi.waitFor(() => expect(w.findAll('.chart-frame')).toHaveLength(2))
    expect(w.find('.v-tab[aria-current="page"]').text()).toBe('State')
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
    expect(w.find('.tyre-chart').exists()).toBe(false)
    expect(mocks.fetchTyres).not.toHaveBeenCalled()
  })

  it('says when nothing is known yet, and when the health cannot be read', async () => {
    mocks.fetchHealth.mockResolvedValue(unknown)
    expect((await mounted()).find('.v-main').text()).toContain('has told nothing of its health yet')
    mocks.fetchHealth.mockRejectedValue(new Error('down'))
    expect((await mounted()).find('.v-alert').text()).toContain('could not be loaded')
  })
})
